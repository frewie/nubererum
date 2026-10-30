package main

import (
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// writeMu runs Things Cloud writes one at a time. Things Cloud accepts a
// write only on top of the latest history state, so parallel writes fail
// with 409 OutdatedAncestor.
var writeMu sync.Mutex

// writeRetryDelays are the pauses before retrying a write that certainly
// did not reach Things Cloud.
var writeRetryDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// retryable reports errors after which nothing was written: the name lookup
// failed, the connection was refused, or Things Cloud rejected the write
// because the history moved on. Timeouts are not retried, because the write
// may have arrived.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, marker := range []string{"OutdatedAncestor", "lookup ", "server misbehaving", "no such host", "Temporary failure in name resolution", "connection refused"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// withWriteRetry runs one write at a time and retries it after errors that
// prove nothing was written.
func withWriteRetry(run func() ([]byte, error)) ([]byte, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	out, err := run()
	for attempt, delay := range writeRetryDelays {
		if !retryable(err) {
			break
		}
		slog.Warn("retrying Things Cloud write", "attempt", attempt+1, "error", redact(err.Error()))
		time.Sleep(delay)
		out, err = run()
	}
	return out, explainWriteError(err)
}

// explainWriteError replaces the SDK's refusal to change the template of a
// repeating to-do with a message for the user. Things keeps the schedule and
// status of a series on its instances; the template (shown in Upcoming as
// "Repeating") only takes a new title, notes and the Trash.
func explainWriteError(err error) error {
	var status statusError
	if errors.As(err, &status) && strings.Contains(status.Message, "is a repeating template") {
		return statusError{Status: http.StatusConflict, Message: "This is the template of a repeating to-do: here you can only rename it, change its notes or move it to the Trash. Change its schedule in Things."}
	}
	return err
}

// moveTarget is where to-dos are dragged: a project, an area or a list.
type moveTarget struct {
	Project string `json:"project"`
	Area    string `json:"area"`
	Heading string `json:"heading"`
	When    string `json:"when"`
}

var movableLists = map[string][]string{
	"today":   {"today", "anytime"},
	"anytime": {"anytime"},
	"someday": {"someday"},
}

// handleMove serves POST /api/move {"uuids":[...],"project"|"area"|"when"}.
func handleMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUIDs []string `json:"uuids"`
		moveTarget
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	targets := 0
	for _, value := range []string{body.Project, body.Area, body.Heading, body.When} {
		if value != "" {
			targets++
		}
	}
	_, listOK := movableLists[body.When]
	if len(body.UUIDs) == 0 || len(body.UUIDs) > 100 || targets != 1 || (body.When != "" && !listOK) ||
		(body.Project != "" && !validThingsID(body.Project)) || (body.Area != "" && !validThingsID(body.Area)) ||
		(body.Heading != "" && !validThingsID(body.Heading)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid move."})
		return
	}
	result, err := models.cached()
	if err != nil {
		errorResponse(w, err)
		return
	}
	tasks, _ := result.model["tasks"].([]map[string]any)
	byUUID := map[string]map[string]any{}
	for _, task := range tasks {
		byUUID[asString(task, "uuid")] = task
	}
	titles := map[string]string{}
	areaOf := map[string]string{}
	projectOf := map[string]string{}
	for _, collection := range []string{"projects", "areas", "headings"} {
		items, _ := result.model[collection].([]map[string]any)
		for _, item := range items {
			titles[asString(item, "uuid")] = asString(item, "title")
			areaOf[asString(item, "uuid")] = asString(item, "area")
			projectOf[asString(item, "uuid")] = asString(item, "project")
		}
	}
	if (body.Project != "" && titles[body.Project] == "") || (body.Area != "" && titles[body.Area] == "") || (body.Heading != "" && titles[body.Heading] == "") {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The project or area changed. Refresh and try again."})
		return
	}

	ops := make([]map[string]any, 0, len(body.UUIDs))
	for _, uuid := range body.UUIDs {
		task, ok := byUUID[uuid]
		if !ok || !validThingsID(uuid) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "The list changed. Refresh and try again."})
			return
		}
		inInbox := slices.Contains(asStringSlice(task["views"]), "inbox")
		switch {
		case body.When != "":
			ops = append(ops, map[string]any{"cmd": "edit", "uuid": uuid, "when": body.When})
		case body.Heading != "" && inInbox:
			// Out of the Inbox into Anytime, as Things does.
			ops = append(ops, map[string]any{"cmd": "edit", "uuid": uuid, "heading": body.Heading})
		case body.Heading != "":
			ops = append(ops, map[string]any{"cmd": "move", "uuid": uuid, "heading": body.Heading})
		case inInbox && body.Project != "":
			// Out of the Inbox into Anytime, as Things does.
			ops = append(ops, map[string]any{"cmd": "move-to-project", "uuid": uuid, "project": body.Project})
		case inInbox:
			ops = append(ops, map[string]any{"cmd": "move-to-area", "uuid": uuid, "area": body.Area})
		case body.Project != "":
			ops = append(ops, map[string]any{"cmd": "move", "uuid": uuid, "project": body.Project})
		default:
			ops = append(ops, map[string]any{"cmd": "move", "uuid": uuid, "area": body.Area})
		}
	}
	edit := func([]string) func(map[string]any) map[string]any {
		return withMovedTasks(body.UUIDs, body.moveTarget, titles, areaOf, projectOf)
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": len(ops)})
}

// withMovedTasks returns a copy of model with the to-dos in their new place
// until the next sync orders them.
func withMovedTasks(uuids []string, target moveTarget, titles, areaOf, projectOf map[string]string) func(map[string]any) map[string]any {
	moved := map[string]bool{}
	for _, uuid := range uuids {
		moved[uuid] = true
	}
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		tasks, _ := model["tasks"].([]map[string]any)
		updated := map[string]map[string]any{}
		nextTasks := make([]map[string]any, len(tasks))
		for i, task := range tasks {
			uuid := asString(task, "uuid")
			if moved[uuid] {
				task = maps.Clone(task)
				views := asStringSlice(task["views"])
				switch {
				case target.When != "":
					views = movableLists[target.When]
				case target.Heading != "":
					project := projectOf[target.Heading]
					task["heading"], task["headingTitle"] = target.Heading, titles[target.Heading]
					task["project"], task["projectTitle"] = project, titles[project]
					task["area"], task["areaTitle"] = areaOf[project], titles[areaOf[project]]
				case target.Project != "":
					task["project"], task["projectTitle"] = target.Project, titles[target.Project]
					task["heading"], task["headingTitle"] = "", ""
					task["area"], task["areaTitle"] = areaOf[target.Project], titles[areaOf[target.Project]]
				default:
					task["project"], task["projectTitle"] = "", ""
					task["heading"], task["headingTitle"] = "", ""
					task["area"], task["areaTitle"] = target.Area, titles[target.Area]
				}
				if slices.Contains(views, "inbox") && target.When == "" {
					views = []string{"anytime"}
				}
				task["views"] = views
				updated[uuid] = task
			}
			nextTasks[i] = task
		}
		next["tasks"] = nextTasks
		if lists, ok := model["lists"].(map[string][]map[string]any); ok {
			nextLists := make(map[string][]map[string]any, len(lists))
			for view, rows := range lists {
				kept := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					uuid := asString(row, "uuid")
					if task, ok := updated[uuid]; ok {
						if slices.Contains(asStringSlice(task["views"]), view) {
							kept = append(kept, task)
						}
						continue
					}
					kept = append(kept, row)
				}
				for uuid, task := range updated {
					if slices.Contains(asStringSlice(task["views"]), view) && !slices.ContainsFunc(kept, func(row map[string]any) bool { return asString(row, "uuid") == uuid }) {
						kept = append(kept, task)
					}
				}
				nextLists[view] = kept
			}
			next["lists"] = nextLists
		}
		return next
	}
}
