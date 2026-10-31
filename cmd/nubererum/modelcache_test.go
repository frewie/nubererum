package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBuild returns models numbered by build and lets a test hold a build
// open until it sends on release.
type fakeBuild struct {
	builds  atomic.Int32
	release chan struct{}
}

func (f *fakeBuild) build() (map[string]any, error) {
	n := f.builds.Add(1)
	if f.release != nil {
		<-f.release
	}
	return map[string]any{
		"build":    int(n),
		"tasks":    []map[string]any{{"uuid": "a", "project": "p"}, {"uuid": "b", "project": "p"}},
		"lists":    map[string][]map[string]any{"today": {{"uuid": "a"}, {"uuid": "b"}}},
		"projects": []map[string]any{{"uuid": "p", "title": "P", "count": 2}},
		"areas":    []map[string]any{},
	}, nil
}

func waitIdle(t *testing.T, c *modelCache) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		idle := c.running == nil
		c.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("sync did not finish")
}

func TestModelCacheSharesOneSync(t *testing.T) {
	fake := &fakeBuild{release: make(chan struct{})}
	cache := newModelCache(fake.build)

	var wg sync.WaitGroup
	results := make([]modelResult, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = cache.cached()
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(fake.release)
	wg.Wait()
	if got := fake.builds.Load(); got != 1 {
		t.Fatalf("builds = %d, want one sync shared by all requests", got)
	}
	for _, result := range results {
		if result.model["build"] != 1 {
			t.Fatalf("result = %v", result.model)
		}
	}
}

func TestModelCacheServesStaleModelAndRefreshesInBackground(t *testing.T) {
	fake := &fakeBuild{}
	cache := newModelCache(fake.build)
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }

	if result, err := cache.cached(); err != nil || result.model["build"] != 1 {
		t.Fatalf("first = %v, %v", result.model, err)
	}
	if result, _ := cache.cached(); result.model["build"] != 1 || fake.builds.Load() != 1 {
		t.Fatal("a fresh cached model was synced again")
	}

	now = now.Add(modelFreshFor + time.Second)
	fake.release = make(chan struct{})
	result, _ := cache.cached()
	if result.model["build"] != 1 || !result.refreshing {
		t.Fatalf("stale request = build %v refreshing %v, want the old model while refreshing", result.model["build"], result.refreshing)
	}
	close(fake.release)
	waitIdle(t, cache)
	if result, _ := cache.cached(); result.model["build"] != 2 {
		t.Fatalf("after refresh = build %v, want 2", result.model["build"])
	}
}

func TestModelCacheFreshWaitsForSyncAfterWrite(t *testing.T) {
	fake := &fakeBuild{}
	cache := newModelCache(fake.build)
	if _, err := cache.cached(); err != nil {
		t.Fatal(err)
	}

	// A sync is running when the write happens; fresh must not return it.
	fake.release = make(chan struct{})
	cache.refresh()
	time.Sleep(10 * time.Millisecond)
	cache.wrote(withoutTask("a"))

	patched, _ := cache.cached()
	if tasks := patched.model["tasks"].([]map[string]any); len(tasks) != 1 || tasks[0]["uuid"] != "b" {
		t.Fatalf("patched tasks = %v, want the completed task removed", tasks)
	}
	if today := patched.model["lists"].(map[string][]map[string]any)["today"]; len(today) != 1 {
		t.Fatalf("patched today = %v", today)
	}
	if projects := patched.model["projects"].([]map[string]any); projects[0]["count"] != 1 {
		t.Fatalf("patched project count = %v, want 1", projects[0]["count"])
	}

	done := make(chan modelResult)
	go func() {
		result, _ := cache.fresh()
		done <- result
	}()
	time.Sleep(20 * time.Millisecond) // let fresh start waiting
	fake.release <- struct{}{}        // finishes the sync that started before the write
	fake.release <- struct{}{}        // finishes the sync after the write
	select {
	case result := <-done:
		if result.model["build"] != 3 {
			t.Fatalf("fresh = build %v, want the sync started after the write", result.model["build"])
		}
	case <-time.After(2 * time.Second):
		cache.mu.Lock()
		running := cache.running != nil
		cache.mu.Unlock()
		t.Fatalf("fresh did not return (builds %d, running %v)", fake.builds.Load(), running)
	}
}

func TestWithoutTaskLeavesOriginalModelUnchanged(t *testing.T) {
	model, _ := (&fakeBuild{}).build()
	next := withoutTask("a")(model)
	if len(model["tasks"].([]map[string]any)) != 2 || model["projects"].([]map[string]any)[0]["count"] != 2 {
		t.Fatalf("original model changed: %v", model)
	}
	if len(next["tasks"].([]map[string]any)) != 1 {
		t.Fatalf("next = %v", next)
	}
}

func TestModelCacheResetDiscardsRunningSync(t *testing.T) {
	fake := &fakeBuild{release: make(chan struct{})}
	cache := newModelCache(fake.build)
	cache.refresh()
	cache.reset()
	close(fake.release)
	time.Sleep(20 * time.Millisecond)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.model != nil {
		t.Fatal("a sync for the previous account filled the cache")
	}
}

func TestGzipResponsesCompressesJSON(t *testing.T) {
	body := strings.Repeat(`{"title":"task"},`, 2000)
	handler := gzipResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	request.Header.Set("Accept-Encoding", "gzip, br")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "gzip" || recorder.Body.Len() >= len(body)/10 {
		t.Fatalf("encoding %q, %d bytes for %d", recorder.Header().Get("Content-Encoding"), recorder.Body.Len(), len(body))
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := io.ReadAll(reader)
	if !strings.Contains(string(decoded), `{\"title\":\"task\"}`) {
		t.Fatalf("decoded body = %.80s", decoded)
	}

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/api/sync", nil))
	if plain.Header().Get("Content-Encoding") != "" || !strings.Contains(plain.Body.String(), "task") {
		t.Fatal("client without gzip support got a compressed body")
	}

	failing := gzipResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "x"})
	}))
	errorResponse := httptest.NewRecorder()
	failing.ServeHTTP(errorResponse, request)
	if errorResponse.Header().Get("Content-Encoding") != "" {
		t.Fatal("error responses should stay uncompressed")
	}
}

// TestMain keeps handler tests from starting real syncs in the background:
// writes trigger a sync of the global model cache.
func TestMain(m *testing.M) {
	// The fake things-cli scripts of most tests run one command per process;
	// TestCLIWorker* turn the worker on.
	os.Setenv("THINGS_CLI_WORKER", "0")
	models = newModelCache(func() (map[string]any, error) {
		return map[string]any{"tasks": []map[string]any{}, "lists": map[string][]map[string]any{}}, nil
	})
	os.Exit(m.Run())
}
