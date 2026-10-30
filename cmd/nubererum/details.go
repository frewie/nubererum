package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
)

// withTasksChanged returns a copy of model in which change has been applied
// to copies of the to-dos uuids, and the lists follow their "views".
func withTasksChanged(uuids []string, change func(task map[string]any)) func(map[string]any) map[string]any {
	selected := map[string]bool{}
	for _, uuid := range uuids {
		selected[uuid] = true
	}
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		tasks, _ := model["tasks"].([]map[string]any)
		updated := map[string]map[string]any{}
		nextTasks := make([]map[string]any, len(tasks))
		for i, task := range tasks {
			if uuid := asString(task, "uuid"); selected[uuid] {
				task = maps.Clone(task)
				change(task)
				updated[uuid] = task
			}
			nextTasks[i] = task
		}
		next["tasks"] = nextTasks
		lists, ok := model["lists"].(map[string][]map[string]any)
		if !ok {
			return next
		}
		nextLists := make(map[string][]map[string]any, len(lists))
		for view, rows := range lists {
			kept := make([]map[string]any, 0, len(rows))
			present := map[string]bool{}
			for _, row := range rows {
				uuid := asString(row, "uuid")
				if task, ok := updated[uuid]; ok {
					if !slices.Contains(asStringSlice(task["views"]), view) {
						continue
					}
					row = task
				}
				present[uuid] = true
				kept = append(kept, row)
			}
			for uuid, task := range updated {
				if !present[uuid] && slices.Contains(asStringSlice(task["views"]), view) {
					kept = append(kept, task)
				}
			}
			nextLists[view] = kept
		}
		next["lists"] = nextLists
		return next
	}
}

// scheduledViews are the lists a to-do shows in after its schedule changed.
func scheduledViews(when, startDate string, task map[string]any) []string {
	today := time.Now().Format("2006-01-02")
	inProject := asString(task, "project") != ""
	switch {
	case startDate != "" && startDate > today:
		return []string{"upcoming"}
	case startDate != "" || when == "today" || when == "evening":
		return []string{"today", "anytime"}
	case when == "anytime":
		return []string{"anytime"}
	case when == "someday" && !inProject:
		return []string{"someday"}
	case when == "inbox":
		return []string{"inbox"}
	}
	return []string{}
}

func validDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// cachedTasks returns the to-dos uuids from the cached model, or false when
// one is unknown.
func cachedTasks(uuids []string) (map[string]map[string]any, bool) {
	result, err := models.cached()
	if err != nil {
		return nil, false
	}
	tasks, _ := result.model["tasks"].([]map[string]any)
	found := map[string]map[string]any{}
	for _, task := range tasks {
		found[asString(task, "uuid")] = task
	}
	selected := map[string]map[string]any{}
	for _, uuid := range uuids {
		task, ok := found[uuid]
		if !ok || !validThingsID(uuid) {
			return nil, false
		}
		selected[uuid] = task
	}
	return selected, true
}

// handleSchedule serves POST /api/schedule {"uuids","when"|"startDate"}.
func handleSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUIDs     []string `json:"uuids"`
		When      string   `json:"when"`
		StartDate string   `json:"startDate"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	validWhenValue := map[string]bool{"today": true, "evening": true, "anytime": true, "someday": true, "inbox": true}[body.When]
	if len(body.UUIDs) == 0 || len(body.UUIDs) > 100 || (body.When == "") == (body.StartDate == "") ||
		(body.When != "" && !validWhenValue) || (body.StartDate != "" && !validDate(body.StartDate)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid schedule."})
		return
	}
	if len(body.UUIDs) == 1 {
		if _, ok := cachedProject(body.UUIDs[0]); ok {
			scheduleProject(w, body.UUIDs[0], body.When, body.StartDate)
			return
		}
	}
	tasks, ok := cachedTasks(body.UUIDs)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The list changed. Refresh and try again."})
		return
	}
	ops := make([]map[string]any, 0, len(body.UUIDs))
	for _, uuid := range body.UUIDs {
		op := map[string]any{"cmd": "schedule", "uuid": uuid}
		if body.When != "" {
			op["when"] = body.When
		} else {
			op["startDate"] = body.StartDate
		}
		ops = append(ops, op)
	}
	today := time.Now().Format("2006-01-02")
	edit := func([]string) func(map[string]any) map[string]any {
		return withTasksChanged(body.UUIDs, func(task map[string]any) {
			task["views"] = scheduledViews(body.When, body.StartDate, tasks[asString(task, "uuid")])
			task["schedule"] = firstString(asStringSlice(task["views"]))
			// This Evening is Today with sb 1; every other schedule ends it.
			task["evening"] = body.When == "evening"
			switch {
			case body.StartDate != "":
				task["scheduledDate"], task["todayDate"] = body.StartDate, body.StartDate
			case body.When == "today" || body.When == "evening":
				task["scheduledDate"], task["todayDate"] = today, today
			default:
				task["scheduledDate"], task["todayDate"] = "", ""
			}
		})
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleDeadline serves POST /api/deadline {"uuid","deadline"}; an empty
// deadline removes it.
func handleDeadline(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID     string `json:"uuid"`
		Deadline string `json:"deadline"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) || (body.Deadline != "" && !validDate(body.Deadline)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid deadline."})
		return
	}
	_, isProject := cachedProject(body.UUID)
	if _, ok := cachedTasks([]string{body.UUID}); !ok && !isProject {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The to-do changed. Refresh and try again."})
		return
	}
	edit := func([]string) func(map[string]any) map[string]any {
		if isProject {
			return withProjectChanged(body.UUID, func(project map[string]any) { project["deadline"] = body.Deadline })
		}
		return withTask(body.UUID, func(task map[string]any) { task["deadline"] = body.Deadline })
	}
	if _, ok := writeBatch(w, []map[string]any{{"cmd": "deadline", "uuid": body.UUID, "deadlineDate": body.Deadline}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTags serves POST /api/tags {"uuid","tags":[tag uuids]}: the tags
// replace the tags of the to-do, project or area.
func handleTags(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string   `json:"uuid"`
		Tags []string `json:"tags"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) || len(body.Tags) > 50 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid tags."})
		return
	}
	result, err := models.cached()
	if err != nil {
		errorResponse(w, err)
		return
	}
	known := map[string]string{}
	tags, _ := result.model["tags"].([]map[string]any)
	for _, tag := range tags {
		known[asString(tag, "uuid")] = asString(tag, "title")
	}
	titles := []string{}
	for _, tag := range body.Tags {
		title, ok := known[tag]
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "A tag changed. Refresh and try again."})
			return
		}
		titles = append(titles, title)
	}
	_, isProject := cachedProject(body.UUID)
	_, isArea := cachedItem("areas", body.UUID)
	if _, ok := cachedTasks([]string{body.UUID}); !ok && !isProject && !isArea {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The to-do changed. Refresh and try again."})
		return
	}
	tagIDs := append([]string{}, body.Tags...)
	collection, command := "projects", "tags"
	if isArea {
		collection, command = "areas", "area-tags"
	}
	edit := func([]string) func(map[string]any) map[string]any {
		if isProject || isArea {
			return withItemChanged(collection, body.UUID, func(item map[string]any) {
				item["tagIDs"], item["tags"] = tagIDs, titles
			})
		}
		return withTask(body.UUID, func(task map[string]any) {
			task["tagIDs"], task["tags"] = tagIDs, titles
		})
	}
	if _, ok := writeBatch(w, []map[string]any{{"cmd": command, "uuid": body.UUID, "tags": tagIDs}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCreateNamed serves POST /api/tag-create and /api/area-create
// {"title"}, which create a tag or an area with things-cli.
func handleCreateNamed(w http.ResponseWriter, r *http.Request, command, collection string) {
	var body struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" || !validText(title, 200) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The name must not be empty or start with --."})
		return
	}
	out, err := withWriteRetry(func() ([]byte, error) { return runCLIOutput(command, title) })
	if err != nil {
		errorResponse(w, err)
		return
	}
	var created struct {
		UUID string `json:"uuid"`
	}
	_ = json.Unmarshal(out, &created)
	item := map[string]any{"uuid": created.UUID, "title": title, "index": 0}
	if collection == "areas" {
		item["type"], item["count"] = "area", 0
	}
	models.wrote(func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		items, _ := model[collection].([]map[string]any)
		next[collection] = append(append([]map[string]any{}, items...), item)
		return next
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uuid": created.UUID, "title": title})
}

// lastHeadingIndex is a position after the last heading of project.
func lastHeadingIndex(project string) (int, bool) {
	result, err := models.cached()
	if err != nil {
		return 0, false
	}
	headings, _ := result.model["headings"].([]map[string]any)
	index, found := 0, false
	for _, heading := range headings {
		if asString(heading, "project") == project {
			if ix := int(asNumber(heading, "index")); !found || ix > index {
				index, found = ix, true
			}
		}
	}
	if !found {
		return 0, false
	}
	return index + reorderStep, true
}

func withNewHeading(heading map[string]any) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		headings, _ := model["headings"].([]map[string]any)
		next["headings"] = append(append([]map[string]any{}, headings...), heading)
		return next
	}
}

// handleAreaRename serves POST /api/area-rename {"uuid","title"}.
func handleAreaRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID  string `json:"uuid"`
		Title string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if !validThingsID(body.UUID) || title == "" || !validText(title, 500) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The name must not be empty or start with --."})
		return
	}
	edit := func([]string) func(map[string]any) map[string]any {
		return withCollectionItem("areas", body.UUID, "areaTitle", &title, nil)
	}
	if _, ok := writeBatch(w, []map[string]any{{"cmd": "area-rename", "uuid": body.UUID, "title": title}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// withCollectionItem returns a copy of model with a new title or note for the
// project or area uuid in collection; a new title also goes to the to-dos'
// titleKey (projectTitle or areaTitle). Models without the item stay as they
// are.
func withCollectionItem(collection, uuid, titleKey string, title, note *string) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		items, _ := model[collection].([]map[string]any)
		index := slices.IndexFunc(items, func(item map[string]any) bool { return asString(item, "uuid") == uuid })
		if index < 0 {
			return model
		}
		next := maps.Clone(model)
		updated := slices.Clone(items)
		updated[index] = maps.Clone(items[index])
		if title != nil {
			updated[index]["title"] = *title
		}
		if note != nil {
			updated[index]["note"] = *note
		}
		next[collection] = updated
		if title == nil {
			return next
		}
		owner := map[string]string{"projects": "project", "areas": "area", "headings": "heading"}[collection]
		// Tasks and lists share rows: each renamed row is copied once.
		copies := map[string]map[string]any{}
		rename := func(rows []map[string]any) []map[string]any {
			result := make([]map[string]any, len(rows))
			for i, row := range rows {
				if asString(row, owner) == uuid {
					id := asString(row, "uuid")
					if copies[id] == nil {
						copies[id] = maps.Clone(row)
						copies[id][titleKey] = *title
					}
					row = copies[id]
				}
				result[i] = row
			}
			return result
		}
		tasks, _ := model["tasks"].([]map[string]any)
		next["tasks"] = rename(tasks)
		if lists, ok := model["lists"].(map[string][]map[string]any); ok {
			nextLists := make(map[string][]map[string]any, len(lists))
			for view, rows := range lists {
				nextLists[view] = rename(rows)
			}
			next["lists"] = nextLists
		}
		return next
	}
}

// handleProjectArea serves POST /api/project-area {"uuid","area"}: the
// project moves into the area, or out of any area when area is empty.
func handleProjectArea(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
		Area string `json:"area"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) || (body.Area != "" && !validThingsID(body.Area)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project or area."})
		return
	}
	result, err := models.cached()
	if err != nil {
		errorResponse(w, err)
		return
	}
	found := map[string]bool{}
	for _, collection := range []string{"projects", "areas"} {
		items, _ := result.model[collection].([]map[string]any)
		for _, item := range items {
			found[collection+asString(item, "uuid")] = true
		}
	}
	if !found["projects"+body.UUID] || (body.Area != "" && !found["areas"+body.Area]) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The sidebar changed. Refresh and try again."})
		return
	}
	op := map[string]any{"cmd": "unfile", "uuid": body.UUID}
	if body.Area != "" {
		op = map[string]any{"cmd": "move", "uuid": body.UUID, "area": body.Area}
	}
	edit := func([]string) func(map[string]any) map[string]any {
		return func(model map[string]any) map[string]any {
			next := maps.Clone(model)
			projects, _ := model["projects"].([]map[string]any)
			updated := make([]map[string]any, len(projects))
			for i, project := range projects {
				if asString(project, "uuid") == body.UUID {
					project = maps.Clone(project)
					project["area"] = body.Area
				}
				updated[i] = project
			}
			next["projects"] = updated
			return next
		}
	}
	if _, ok := writeBatch(w, []map[string]any{op}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// reminderTime returns a to-do's reminder as HH:MM: ato (AlarmTimeOffset)
// counts the seconds after midnight of its start date.
func reminderTime(task map[string]any) string {
	seconds, ok := task["AlarmTimeOffset"].(float64)
	if !ok || seconds < 0 || seconds >= 86400 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", int(seconds)/3600, int(seconds)%3600/60)
}

// handleReminder serves POST /api/reminder {"uuid","time":"HH:MM"|""}. Like
// Things, a reminder rings on the to-do's start date, so it needs one: Today
// (also This Evening) or a later date; an empty time removes it.
func handleReminder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
		Time string `json:"time"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if _, err := time.Parse("15:04", body.Time); !validThingsID(body.UUID) || (body.Time != "" && (err != nil || len(body.Time) != 5)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid reminder time."})
		return
	}
	tasks, ok := cachedTasks([]string{body.UUID})
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The to-do changed. Refresh and try again."})
		return
	}
	task := tasks[body.UUID]
	op := map[string]any{"cmd": "reminder", "uuid": body.UUID, "time": body.Time}
	if body.Time != "" {
		today := time.Now().Format("2006-01-02")
		date := asString(task, "scheduledDate")
		switch {
		case slices.Contains(asStringSlice(task["views"]), "today"):
			date = today
		case date <= today:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A reminder needs a date: Today, This Evening or a start date."})
			return
		}
		op["startDate"] = date
	}
	edit := func([]string) func(map[string]any) map[string]any {
		return withTask(body.UUID, func(task map[string]any) { task["reminder"] = body.Time })
	}
	if _, ok := writeBatch(w, []map[string]any{op}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
