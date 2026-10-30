package main

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

// projectProgress counts the to-dos of every project for the progress pie:
// total are those not in the Trash and not canceled, done the completed
// ones, logged ones included, as Things counts them.
func projectProgress(tasks map[string]map[string]any) map[string][2]int {
	result := map[string][2]int{}
	for id, task := range tasks {
		if int(asNumber(task, "Type")) != 0 || asBool(task, "InTrash") {
			continue
		}
		status := int(asNumber(task, "Status"))
		if status == 2 {
			continue
		}
		project := findProjectID(id, tasks)
		if project == "" {
			continue
		}
		counts := result[project]
		counts[1]++
		if status == 3 {
			counts[0]++
		}
		result[project] = counts
	}
	return result
}

// projectStatus names a project's state for the model.
func projectStatus(project map[string]any) string {
	switch int(asNumber(project, "Status")) {
	case 2:
		return "canceled"
	case 3:
		return "completed"
	}
	return "open"
}

// cachedProject returns the project uuid from the cached model.
func cachedProject(uuid string) (map[string]any, bool) {
	return cachedItem("projects", uuid)
}

// cachedItem returns the project or area uuid (collection "projects" or
// "areas") of the cached model.
func cachedItem(collection, uuid string) (map[string]any, bool) {
	result, err := models.cached()
	if err != nil {
		return nil, false
	}
	items, _ := result.model[collection].([]map[string]any)
	for _, item := range items {
		if asString(item, "uuid") == uuid {
			return item, true
		}
	}
	return nil, false
}

// withProjectChanged returns a patch that changes a copy of the project.
func withProjectChanged(uuid string, change func(project map[string]any)) func(map[string]any) map[string]any {
	return withItemChanged("projects", uuid, change)
}

// withItemChanged returns a patch that changes a copy of the project or area
// uuid in collection.
func withItemChanged(collection, uuid string, change func(item map[string]any)) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		items, _ := model[collection].([]map[string]any)
		next := maps.Clone(model)
		copied := make([]map[string]any, len(items))
		for i, item := range items {
			if asString(item, "uuid") == uuid {
				item = maps.Clone(item)
				change(item)
			}
			copied[i] = item
		}
		next[collection] = copied
		return next
	}
}

// projectOpenToDos returns the open to-dos of a project (also under its
// headings) and their open checklist items, from the CLI cache, sorted.
func projectOpenToDos(uuid string) (todos []string, items map[string][]string) {
	cache, err := readCache()
	if err != nil {
		return nil, nil
	}
	items = map[string][]string{}
	for id, task := range cache.State.Tasks {
		if isOpenCacheItem(task, 0) && validThingsID(id) && findProjectID(id, cache.State.Tasks) == uuid {
			todos = append(todos, id)
		}
	}
	sort.Strings(todos)
	for id, item := range cache.State.CheckListItems {
		taskIDs := asStringSlice(item["TaskIDs"])
		if len(taskIDs) == 0 || int(asNumber(item, "Status")) != 0 || !validThingsID(id) {
			continue
		}
		if slices.Contains(todos, taskIDs[0]) {
			items[taskIDs[0]] = append(items[taskIDs[0]], id)
		}
	}
	for _, list := range items {
		sort.Strings(list)
	}
	return todos, items
}

// handleProjectComplete serves POST /api/project-complete
// {"uuid","mode":"complete"|"cancel","rest":"complete"|"cancel"}. Like
// Things, it first completes or cancels the open to-dos (completing also
// ticks off their checklists), then the project, in one write. The project
// and its to-dos stay visible until "Log Completed".
func handleProjectComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
		Mode string `json:"mode"`
		Rest string `json:"rest"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if body.Rest == "" {
		body.Rest = body.Mode
	}
	validMode := func(mode string) bool { return mode == "complete" || mode == "cancel" }
	if !validThingsID(body.UUID) || !validMode(body.Mode) || !validMode(body.Rest) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project or choice."})
		return
	}
	if project, ok := cachedProject(body.UUID); !ok || asString(project, "status") != "open" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The project changed. Refresh and try again."})
		return
	}
	todos, items := projectOpenToDos(body.UUID)
	ops := []map[string]any{}
	for _, todo := range todos {
		if body.Rest == "complete" {
			for _, item := range items[todo] {
				ops = append(ops, map[string]any{"cmd": "checklist-complete", "uuid": item, "done": true})
			}
		}
		ops = append(ops, map[string]any{"cmd": body.Rest, "uuid": todo})
	}
	ops = append(ops, map[string]any{"cmd": body.Mode, "uuid": body.UUID})
	status := map[string]string{"complete": "completed", "cancel": "canceled"}
	edit := func([]string) func(map[string]any) map[string]any {
		return func(model map[string]any) map[string]any {
			model = withProjectChanged(body.UUID, func(project map[string]any) {
				project["status"] = status[body.Mode]
				if body.Rest == "complete" {
					project["done"] = asNumber(project, "total")
				}
			})(model)
			return withTasksChanged(todos, func(task map[string]any) {
				if asString(task, "status") == "open" {
					task["status"] = status[body.Rest]
				}
			})(model)
		}
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "todos": len(todos)})
}

// scheduleProject sets when an open project starts. Projects live in
// Anytime (st 1) or Someday (st 2), never in the Inbox, and have no This
// Evening.
func scheduleProject(w http.ResponseWriter, uuid, when, startDate string) {
	if when == "inbox" || when == "evening" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A project cannot go there."})
		return
	}
	op := map[string]any{"cmd": "schedule", "uuid": uuid}
	if when != "" {
		op["when"] = when
	} else {
		op["startDate"] = startDate
	}
	today := time.Now().Format("2006-01-02")
	edit := func([]string) func(map[string]any) map[string]any {
		return withProjectChanged(uuid, func(project map[string]any) {
			switch {
			case startDate != "" && startDate > today:
				project["when"], project["scheduledDate"] = "upcoming", startDate
			case startDate != "":
				project["when"], project["scheduledDate"] = "anytime", startDate
			case when == "today":
				project["when"], project["scheduledDate"] = "anytime", today
			default:
				project["when"], project["scheduledDate"] = when, ""
			}
		})
	}
	if _, ok := writeBatch(w, []map[string]any{op}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleHeadingDelete serves POST /api/heading-delete {"uuid"}. Like Things,
// the heading's to-dos stay in the project ({agr:[], pr:[project]}), then
// the heading is deleted (action 2), in one write.
func handleHeadingDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid heading."})
		return
	}
	project := ""
	if result, err := models.cached(); err == nil {
		headings, _ := result.model["headings"].([]map[string]any)
		for _, heading := range headings {
			if asString(heading, "uuid") == body.UUID {
				project = asString(heading, "project")
			}
		}
	}
	if !validThingsID(project) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The heading changed. Refresh and try again."})
		return
	}
	// Every to-do under the heading moves, also completed and logged ones,
	// so none points to the deleted heading.
	todos := []string{}
	if cache, err := readCache(); err == nil {
		for id, task := range cache.State.Tasks {
			if int(asNumber(task, "Type")) == 0 && firstString(asStringSlice(task["ActionGroupIDs"])) == body.UUID && validThingsID(id) {
				todos = append(todos, id)
			}
		}
	}
	sort.Strings(todos)
	ops := make([]map[string]any, 0, len(todos)+1)
	for _, todo := range todos {
		ops = append(ops, map[string]any{"cmd": "move", "uuid": todo, "project": project})
	}
	ops = append(ops, map[string]any{"cmd": "delete", "uuid": body.UUID})
	edit := func([]string) func(map[string]any) map[string]any {
		return func(model map[string]any) map[string]any {
			next := maps.Clone(model)
			if headings, ok := model["headings"].([]map[string]any); ok {
				next["headings"] = slices.DeleteFunc(slices.Clone(headings), func(heading map[string]any) bool { return asString(heading, "uuid") == body.UUID })
			}
			tasks, _ := model["tasks"].([]map[string]any)
			under := []string{}
			for _, task := range tasks {
				if asString(task, "heading") == body.UUID {
					under = append(under, asString(task, "uuid"))
				}
			}
			return withTasksChanged(under, func(task map[string]any) {
				task["heading"], task["headingTitle"] = "", ""
			})(next)
		}
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "todos": len(todos)})
}

// handleAreaDelete serves POST /api/area-delete {"uuid"}. Like current
// Things, the area's projects and to-dos go to the Trash ({tr:true}; a
// project takes its to-dos along), then the area is deleted (Area3 action
// 2), in one write. Restoring the trashed items does not bring the area back.
func handleAreaDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid area."})
		return
	}
	found := false
	if result, err := models.cached(); err == nil {
		areas, _ := result.model["areas"].([]map[string]any)
		found = slices.ContainsFunc(areas, func(area map[string]any) bool { return asString(area, "uuid") == body.UUID })
	}
	if !found {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The area changed. Refresh and try again."})
		return
	}
	// Every project and to-do filed in the area, also completed and logged
	// ones, so none points to the deleted area.
	trashed := []string{}
	if cache, err := readCache(); err == nil {
		for id, task := range cache.State.Tasks {
			kind := int(asNumber(task, "Type"))
			if (kind == 0 || kind == 1) && !asBool(task, "InTrash") && firstString(asStringSlice(task["AreaIDs"])) == body.UUID && validThingsID(id) {
				trashed = append(trashed, id)
			}
		}
	}
	sort.Strings(trashed)
	ops := make([]map[string]any, 0, len(trashed)+1)
	for _, id := range trashed {
		ops = append(ops, map[string]any{"cmd": "trash", "uuid": id})
	}
	ops = append(ops, projectToDoOps(trashed, "trash", false)...)
	ops = append(ops, map[string]any{"cmd": "area-delete", "uuid": body.UUID})
	edit := func([]string) func(map[string]any) map[string]any {
		return func(model map[string]any) map[string]any {
			for _, key := range []string{"projects", "tasks"} {
				rows, _ := model[key].([]map[string]any)
				for _, row := range rows {
					if asString(row, "area") == body.UUID {
						uuid := asString(row, "uuid")
						model = withoutProject(uuid)(withoutTask(uuid)(model))
					}
				}
			}
			next := maps.Clone(model)
			if areas, ok := model["areas"].([]map[string]any); ok {
				next["areas"] = slices.DeleteFunc(slices.Clone(areas), func(area map[string]any) bool { return asString(area, "uuid") == body.UUID })
			}
			return next
		}
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "trashed": trashed, "count": len(ops) - 1})
}

// runOps writes ops in one batch and returns the uuids the CLI reports, in
// order (new items for creates).
func runOps(ops []map[string]any) ([]string, error) {
	input, _ := json.Marshal(ops)
	out, err := runCLIBatch(input)
	if err != nil {
		return nil, err
	}
	var reported struct {
		Results []struct {
			UUID string `json:"uuid"`
		} `json:"results"`
	}
	_ = json.Unmarshal(out, &reported)
	ids := make([]string, len(reported.Results))
	for i, result := range reported.Results {
		ids[i] = result.UUID
	}
	return ids, nil
}

// handleConvertProject serves POST /api/convert-project {"uuid"}: like
// Things' "Convert to Project", the to-do becomes a project with its title,
// notes, area, When, deadline and tags; its checklist items become the
// project's to-dos (done ones completed). The history shows no write that
// turns a to-do into a project, so this is built from verified writes: the
// project, then its to-dos, then their order, the completed ones and the
// original to the Trash. A failed step stops and reports what exists; the
// original stays until the last step.
func handleConvertProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid to-do."})
		return
	}
	tasks, ok := cachedTasks([]string{body.UUID})
	if !ok || asString(tasks[body.UUID], "status") != "open" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The to-do changed. Refresh and try again."})
		return
	}
	task := tasks[body.UUID]
	if asString(task, "repeat") != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "A repeating to-do cannot become a project."})
		return
	}
	note := asString(task, "note")
	if !validText(note, 5000) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The notes are too long to copy into a project."})
		return
	}
	project := map[string]any{"cmd": "create", "type": "project", "title": asString(task, "title")}
	if note != "" {
		project["note"] = note
	}
	if area := asString(task, "area"); validThingsID(area) {
		project["area"] = area
	}
	// Projects have no Inbox and no This Evening.
	views := asStringSlice(task["views"])
	switch {
	case slices.Contains(views, "upcoming") && validDate(asString(task, "scheduledDate")):
		project["extra"] = map[string]string{"scheduled": asString(task, "scheduledDate")}
	case slices.Contains(views, "today"):
		project["when"] = "today"
	case slices.Contains(views, "someday"):
		project["when"] = "someday"
	default:
		project["when"] = "anytime"
	}
	if deadline := asString(task, "deadline"); validDate(deadline) {
		project["deadline"] = deadline
	}
	if tags := asStringSlice(task["tagIDs"]); len(tags) > 0 {
		project["tags"] = tags
	}
	failed := func(step, created string, err error) {
		models.wrote(nil)
		message := "Could not convert the to-do (" + step + "): " + err.Error()
		if created != "" {
			message += " The new project exists; the to-do is unchanged."
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": message, "uuid": created})
	}
	ids, err := runOps([]map[string]any{project})
	if err != nil || len(ids) == 0 || !validThingsID(ids[0]) {
		if err == nil {
			err = errors.New("no project id")
		}
		failed("project", "", err)
		return
	}
	projectID := ids[0]
	checklist, _ := task["checklist"].([]map[string]any)
	items, creates := []map[string]any{}, []map[string]any{}
	for _, item := range checklist {
		if title := strings.TrimSpace(asString(item, "title")); title != "" && validText(title, 500) {
			items = append(items, item)
			creates = append(creates, map[string]any{"cmd": "create", "title": title, "project": projectID, "when": "anytime"})
		}
	}
	finish := []map[string]any{}
	if len(creates) > 0 {
		ids, err := runOps(creates)
		if err != nil || len(ids) != len(creates) {
			if err == nil {
				err = errors.New("missing to-do ids")
			}
			failed("checklist to-dos", projectID, err)
			return
		}
		// Creates start at position 0; keep the checklist's order.
		for at, item := range items {
			finish = append(finish, map[string]any{"cmd": "reorder", "uuid": ids[at], "index": at})
			if asBool(item, "done") {
				finish = append(finish, map[string]any{"cmd": "complete", "uuid": ids[at]})
			}
		}
	}
	finish = append(finish, map[string]any{"cmd": "trash", "uuid": body.UUID})
	if _, err := runOps(finish); err != nil {
		failed("order and Trash", projectID, err)
		return
	}
	models.wrote(withoutTask(body.UUID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uuid": projectID, "todos": len(creates)})
}
