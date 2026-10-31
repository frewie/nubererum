package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"
)

// reorderStep is the gap left between positions, like the gaps Things leaves.
const reorderStep = 512

const maxReorderSection = 1000

// reorderOp is one things-cli batch "reorder" operation.
type reorderOp struct {
	Cmd        string `json:"cmd"`
	UUID       string `json:"uuid"`
	Index      *int   `json:"index,omitempty"`
	TodayIndex *int   `json:"todayIndex,omitempty"`
	TodayDate  string `json:"todayDate,omitempty"`
}

type reorderTask struct {
	uuid      string
	position  int    // ix, or ti in Today
	todayDate string // tir, Today only
}

// planReorder returns the writes that move moved to its place in order, the
// new order of one section. Like Things, it gives the moved to-do a position
// between its neighbours and renumbers the section only when there is no
// room. In Today, a to-do moved next to one that entered Today on another day
// takes over that day, because Today sorts by that day first.
func planReorder(order []reorderTask, moved string, today bool) ([]reorderOp, error) {
	at := -1
	for i, task := range order {
		if task.uuid == moved {
			at = i
		}
	}
	if at < 0 {
		return nil, errors.New("the moved to-do is not in the new order")
	}
	current := order[at]
	var prev, next *reorderTask
	if at > 0 {
		prev = &order[at-1]
	}
	if at+1 < len(order) {
		next = &order[at+1]
	}
	if today {
		switch {
		case prev != nil:
			current.todayDate = prev.todayDate
		case next != nil:
			current.todayDate = next.todayDate
		}
		// Only neighbours from the same day decide the position.
		if prev != nil && prev.todayDate != current.todayDate {
			prev = nil
		}
		if next != nil && next.todayDate != current.todayDate {
			next = nil
		}
	}

	// op writes tir only when the day changes; Things usually writes ti alone.
	op := func(uuid string, position int, todayDate, previousDate string) reorderOp {
		value := position
		result := reorderOp{Cmd: "reorder", UUID: uuid}
		if today {
			result.TodayIndex = &value
			if todayDate != previousDate {
				result.TodayDate = todayDate
			}
		} else {
			result.Index = &value
		}
		return result
	}

	switch {
	case prev != nil && next != nil && next.position-prev.position >= 2:
		return []reorderOp{op(moved, prev.position+(next.position-prev.position)/2, current.todayDate, order[at].todayDate)}, nil
	case prev != nil && next == nil:
		return []reorderOp{op(moved, prev.position+reorderStep, current.todayDate, order[at].todayDate)}, nil
	case prev == nil && next != nil:
		return []reorderOp{op(moved, next.position-reorderStep, current.todayDate, order[at].todayDate)}, nil
	case prev == nil && next == nil:
		if today && current.todayDate != order[at].todayDate {
			return []reorderOp{op(moved, order[at].position, current.todayDate, order[at].todayDate)}, nil
		}
		return nil, nil
	}

	// No room between the neighbours: renumber the moved to-do's group in the
	// new order, starting at the first position, and write what changed.
	group := []reorderTask{}
	previousDate := map[string]string{}
	for i, task := range order {
		previousDate[task.uuid] = task.todayDate
		if i == at {
			task = current
		}
		if !today || task.todayDate == current.todayDate {
			group = append(group, task)
		}
	}
	start := group[0].position
	for _, task := range group {
		if task.position < start {
			start = task.position
		}
	}
	ops := []reorderOp{}
	for i, task := range group {
		position := start + i*reorderStep
		if position != task.position || task.uuid == moved {
			ops = append(ops, op(task.uuid, position, task.todayDate, previousDate[task.uuid]))
		}
	}
	return ops, nil
}

// handleReorder serves POST /api/reorder {"view","moved","order"}: order is
// the new order of the section the to-do was dragged in.
func handleReorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		View  string   `json:"view"`
		Moved string   `json:"moved"`
		Order []string `json:"order"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if len(body.Order) < 2 || len(body.Order) > maxReorderSection || !validThingsID(body.Moved) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid order."})
		return
	}
	result, err := models.cached()
	if err != nil {
		errorResponse(w, err)
		return
	}
	// Projects are reordered in the sidebar; they are to-dos of type project.
	key := "tasks"
	switch body.View {
	case "projects":
		key = "projects"
	case "areas":
		key = "areas"
	}
	tasks, _ := result.model[key].([]map[string]any)
	byUUID := make(map[string]map[string]any, len(tasks))
	for _, task := range tasks {
		byUUID[asString(task, "uuid")] = task
	}
	today := body.View == "today"
	seen := map[string]bool{}
	order := make([]reorderTask, 0, len(body.Order))
	for _, uuid := range body.Order {
		task, ok := byUUID[uuid]
		if !ok || !validThingsID(uuid) || seen[uuid] {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "The list changed. Refresh and try again."})
			return
		}
		seen[uuid] = true
		entry := reorderTask{uuid: uuid, position: int(asNumber(task, "index"))}
		if today {
			entry.position = int(asNumber(task, "todayIndex"))
			entry.todayDate = asString(task, "todayDate")
			if _, err := time.Parse("2006-01-02", entry.todayDate); err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "This to-do cannot be moved in Today."})
				return
			}
		}
		order = append(order, entry)
	}
	ops, err := planReorder(order, body.Moved, today)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.View == "areas" {
		for i := range ops {
			ops[i].Cmd = "area-reorder"
		}
	}
	if len(ops) > 0 {
		input, _ := json.Marshal(ops)
		if _, err := runCLIBatch(input); err != nil {
			errorResponse(w, err)
			return
		}
		if body.View == "projects" || body.View == "areas" {
			models.wrote(withPositions(key, ops))
		} else {
			models.wrote(nil)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reordered": true, "writes": len(ops)})
}

// runCLIBatch runs `things-cli batch` with ops as JSON on stdin.
func runCLIBatch(input []byte) ([]byte, error) {
	sessionMu.RLock()
	email := session.Email
	password := session.Password
	sessionMu.RUnlock()
	out, err := withWriteRetry(func() ([]byte, error) { return execCLI(email, password, input, false, "batch") })
	if err != nil {
		var statusErr statusError
		if errors.As(err, &statusErr) && statusErr.Status != http.StatusBadGateway {
			return nil, statusErr
		}
		return nil, statusError{Status: http.StatusBadGateway, Message: fmt.Sprintf("Things Cloud write failed: %s", redact(strings.TrimSpace(err.Error())))}
	}
	return out, nil
}

// withPositions applies new project or area positions to a copy of model,
// so the sidebar keeps the new order until the next sync.
func withPositions(collection string, ops []reorderOp) func(map[string]any) map[string]any {
	positions := map[string]int{}
	for _, op := range ops {
		if op.Index != nil {
			positions[op.UUID] = *op.Index
		}
	}
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		items, _ := model[collection].([]map[string]any)
		updated := make([]map[string]any, len(items))
		for i, item := range items {
			if position, ok := positions[asString(item, "uuid")]; ok {
				item = maps.Clone(item)
				item["index"] = position
			}
			updated[i] = item
		}
		next[collection] = updated
		return next
	}
}
