package main

import (
	"maps"
	"sync"
	"time"
)

// modelFreshFor is how long a cached model is served without starting a
// background refresh.
const modelFreshFor = 30 * time.Second

// modelCache keeps the last synced model so pages render at once while a
// refresh runs in the background. At most one sync runs at a time, because
// concurrent things-cli runs would race on the state cache file. Stored
// models are never modified; changes replace them.
type modelCache struct {
	build func() (map[string]any, error)
	now   func() time.Time

	mu         sync.Mutex
	model      map[string]any
	syncedAt   time.Time
	lastStart  time.Time // start of the newest sync, running or finished
	lastErr    error
	lastWrite  time.Time
	generation int
	running    *syncRun
}

type syncRun struct {
	done      chan struct{}
	startedAt time.Time
	model     map[string]any
	err       error
}

func newModelCache(build func() (map[string]any, error)) *modelCache {
	return &modelCache{build: build, now: time.Now}
}

var models = newModelCache(syncModel)

// startLocked starts a sync unless one is running and returns the run.
func (c *modelCache) startLocked() *syncRun {
	if c.running != nil {
		return c.running
	}
	run := &syncRun{done: make(chan struct{}), startedAt: c.now()}
	c.running = run
	c.lastStart = run.startedAt
	generation := c.generation
	go func() {
		model, err := c.build()
		c.mu.Lock()
		run.model, run.err = model, err
		if generation == c.generation {
			if err == nil {
				c.model, c.syncedAt, c.lastErr = model, c.now(), nil
			} else {
				c.lastErr = err
				logError("sync failed", err)
			}
		}
		if c.running == run {
			c.running = nil
		}
		c.mu.Unlock()
		close(run.done)
	}()
	return run
}

// refresh starts a background sync if none is running.
func (c *modelCache) refresh() {
	c.mu.Lock()
	c.startLocked()
	c.mu.Unlock()
}

type modelResult struct {
	model      map[string]any
	syncedAt   time.Time
	refreshing bool
	syncErr    error
}

// cached returns the cached model at once and refreshes it in the background
// when it is older than modelFreshFor. Without a cached model it waits for a
// sync.
func (c *modelCache) cached() (modelResult, error) {
	c.mu.Lock()
	if c.model == nil {
		run := c.startLocked()
		c.mu.Unlock()
		<-run.done
		return c.wait(run)
	}
	if c.now().Sub(c.syncedAt) > modelFreshFor {
		c.startLocked()
	}
	result := modelResult{model: c.model, syncedAt: c.syncedAt, refreshing: c.running != nil, syncErr: c.lastErr}
	c.mu.Unlock()
	return result, nil
}

// fresh returns the result of a sync that started after the last write, so
// it includes changes made through the app. It joins a running sync that
// qualifies, reuses one that started after the call and has finished, and
// otherwise starts one.
func (c *modelCache) fresh() (modelResult, error) {
	called := c.now()
	for {
		c.mu.Lock()
		run := c.running
		switch {
		case run != nil && !run.startedAt.Before(c.lastWrite):
		case run == nil && c.model != nil && c.lastErr == nil && !c.lastStart.Before(called) && !c.lastStart.Before(c.lastWrite):
			result := modelResult{model: c.model, syncedAt: c.syncedAt}
			c.mu.Unlock()
			return result, nil
		case run == nil:
			run = c.startLocked()
		default:
			// The running sync started before the last write: wait, then retry.
			c.mu.Unlock()
			<-run.done
			continue
		}
		c.mu.Unlock()
		<-run.done
		return c.wait(run)
	}
}

func (c *modelCache) wait(run *syncRun) (modelResult, error) {
	if run.err != nil {
		return modelResult{}, run.err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return modelResult{model: run.model, syncedAt: c.syncedAt}, nil
}

// wrote records a change made through the app and starts a sync that sees
// it. edit, if set, patches the cached model until then.
func (c *modelCache) wrote(edit func(map[string]any) map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastWrite = c.now()
	if edit != nil && c.model != nil {
		c.model = edit(c.model)
	}
	if c.running == nil {
		c.startLocked()
		return
	}
	// The running sync may have missed the change: sync again after it.
	run := c.running
	go func() {
		<-run.done
		c.mu.Lock()
		if c.running == nil && c.lastStart.Before(c.lastWrite) {
			c.startLocked()
		}
		c.mu.Unlock()
	}()
}

// reset drops the cached model, e.g. when the Things account changes. A sync
// that is still running for the old account is discarded.
func (c *modelCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.model, c.syncedAt, c.lastErr = nil, time.Time{}, nil
	c.running = nil
}

// withoutTask returns a copy of model without the task uuid. model itself is
// left unchanged because responses may still be encoding it.
func withoutTask(uuid string) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		keep := func(rows []map[string]any) []map[string]any {
			result := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				if asString(row, "uuid") != uuid {
					result = append(result, row)
				}
			}
			return result
		}
		tasks, _ := model["tasks"].([]map[string]any)
		next["tasks"] = keep(tasks)
		if lists, ok := model["lists"].(map[string][]map[string]any); ok {
			nextLists := make(map[string][]map[string]any, len(lists))
			for view, rows := range lists {
				nextLists[view] = keep(rows)
			}
			next["lists"] = nextLists
		}
		recountCollections(next, next["tasks"].([]map[string]any))
		return next
	}
}
