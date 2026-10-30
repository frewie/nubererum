package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const archiveLimit = 300

func readCache() (cachedStateFile, error) {
	var cache cachedStateFile
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return cache, err
	}
	err = json.Unmarshal(data, &cache)
	return cache, err
}

// archiveRow is a Logbook or Trash entry.
func archiveRow(id string, task map[string]any, cache cachedStateFile) map[string]any {
	projectID := findProjectID(id, cache.State.Tasks)
	areaID := firstString(asStringSlice(task["AreaIDs"]))
	if areaID == "" && projectID != "" {
		areaID = firstString(asStringSlice(cache.State.Tasks[projectID]["AreaIDs"]))
	}
	status := "open"
	switch int(asNumber(task, "Status")) {
	case 3:
		status = "completed"
	case 2:
		status = "canceled"
	}
	kind := "task"
	if int(asNumber(task, "Type")) == 1 {
		kind = "project"
	}
	return map[string]any{
		"uuid":         id,
		"title":        strings.TrimSpace(asString(task, "Title")),
		"type":         kind,
		"status":       status,
		"completedAt":  asString(task, "CompletionDate"),
		"modifiedAt":   asString(task, "ModificationDate"),
		"project":      projectID,
		"projectTitle": strings.TrimSpace(asString(cache.State.Tasks[projectID], "Title")),
		"areaTitle":    strings.TrimSpace(asString(cache.State.Areas[areaID], "Title")),
	}
}

// handleArchive serves GET /api/logbook and /api/trash: completed to-dos and
// projects (newest first) or everything in the Trash (latest change first).
func handleArchive(w http.ResponseWriter, r *http.Request, trash bool) {
	cache, err := readCache()
	if err != nil {
		errorResponse(w, err)
		return
	}
	limit := archiveLimit
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 2000 {
		limit = value
	}
	logbook := newLogbookRule(cache.State.LogDate, cache.State.LogInterval, time.Now())
	rows := []map[string]any{}
	for id, task := range cache.State.Tasks {
		kind := int(asNumber(task, "Type"))
		if kind == 2 || strings.TrimSpace(asString(task, "Title")) == "" {
			continue
		}
		inTrash := asBool(task, "InTrash")
		status := int(asNumber(task, "Status"))
		switch {
		case trash && inTrash:
		case !trash && !inTrash && (status == 2 || status == 3) && !logbook.stillListed(task):
		default:
			continue
		}
		rows = append(rows, archiveRow(id, task, cache))
	}
	key := "completedAt"
	if trash {
		key = "modifiedAt"
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := asString(rows[i], key), asString(rows[j], key); a != b {
			return a > b
		}
		return asString(rows[i], "uuid") < asString(rows[j], "uuid")
	})
	total := len(rows)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "total": total})
}

// emptyTrashBatch is how many items one Empty Trash write deletes (a
// variable for tests).
var emptyTrashBatch = 50

// trashedItems returns everything Empty Trash deletes: the items in the Trash
// and the to-dos and headings of trashed projects, children before their
// project so an interrupted run leaves nothing pointing to a deleted one.
func trashedItems(tasks map[string]map[string]any) []string {
	rank := map[int]int{0: 0, 2: 1, 1: 2}
	ids := []string{}
	for id, task := range tasks {
		if !validThingsID(id) {
			continue
		}
		if asBool(task, "InTrash") || asBool(tasks[findProjectID(id, tasks)], "InTrash") {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := rank[int(asNumber(tasks[ids[i]], "Type"))], rank[int(asNumber(tasks[ids[j]], "Type"))]
		if a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	return ids
}

// handleEmptyTrash serves POST /api/empty-trash. Like Things, every item is
// deleted for good (Task7 action 2 {} plus a Tombstone2 naming it), in
// writes of emptyTrashBatch items, one after the other. Their checklist items
// stay in the history, as Things leaves them.
func handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	cache, err := readCache()
	if err != nil {
		errorResponse(w, err)
		return
	}
	ids := trashedItems(cache.State.Tasks)
	deleted := 0
	for start := 0; start < len(ids); start += emptyTrashBatch {
		chunk := ids[start:min(start+emptyTrashBatch, len(ids))]
		ops := make([]map[string]any, 0, 2*len(chunk))
		for _, id := range chunk {
			ops = append(ops, map[string]any{"cmd": "delete", "uuid": id}, map[string]any{"cmd": "purge", "uuid": id})
		}
		input, _ := json.Marshal(ops)
		if _, err := runCLIBatch(input); err != nil {
			if deleted > 0 {
				models.wrote(nil)
			}
			slog.Warn("empty trash stopped", "deleted", deleted, "left", len(ids)-deleted)
			errorResponse(w, err)
			return
		}
		deleted += len(chunk)
	}
	if deleted > 0 {
		models.wrote(nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

// searchLimit caps the Logbook matches of one search.
const searchLimit = 100

// handleSearch serves GET /api/search?q= (2 to 200 characters): the Logbook
// to-dos and projects whose title or notes contain q, ignoring case, newest
// first, at most searchLimit. Open items are found in the page's model;
// nothing in the Trash is returned.
func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if n := utf8.RuneCountInString(query); n < 2 || n > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Search for 2 to 200 characters."})
		return
	}
	cache, err := readCache()
	if err != nil {
		errorResponse(w, err)
		return
	}
	logbook := newLogbookRule(cache.State.LogDate, cache.State.LogInterval, time.Now())
	rows := []map[string]any{}
	for id, task := range cache.State.Tasks {
		kind := int(asNumber(task, "Type"))
		status := int(asNumber(task, "Status"))
		if kind == 2 || asBool(task, "InTrash") || (status != 2 && status != 3) || logbook.stillListed(task) {
			continue
		}
		text := strings.ToLower(asString(task, "Title") + "\n" + asString(task, "Note"))
		if strings.TrimSpace(asString(task, "Title")) == "" || !strings.Contains(text, query) {
			continue
		}
		rows = append(rows, archiveRow(id, task, cache))
	}
	sort.Slice(rows, func(i, j int) bool {
		if a, b := asString(rows[i], "completedAt"), asString(rows[j], "completedAt"); a != b {
			return a > b
		}
		return asString(rows[i], "uuid") < asString(rows[j], "uuid")
	})
	total := len(rows)
	if len(rows) > searchLimit {
		rows = rows[:searchLimit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "total": total})
}
