package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"sort"
	"strings"
	"time"
)

// writePaths change Things Cloud; APP_READ_ONLY rejects them.
var writePaths = map[string]bool{
	"/api/complete": true, "/api/create": true, "/api/reorder": true,
	"/api/update": true, "/api/checklist": true, "/api/trash": true, "/api/restore": true,
	"/api/reopen": true, "/api/cancel": true, "/api/project-complete": true, "/api/heading-delete": true, "/api/log-completed": true, "/api/move": true,
	"/api/schedule": true, "/api/deadline": true, "/api/project-area": true, "/api/tags": true, "/api/tag-create": true, "/api/area-create": true, "/api/area-rename": true, "/api/area-delete": true, "/api/empty-trash": true, "/api/reminder": true, "/api/repeat": true, "/api/tag-rename": true, "/api/tag-delete": true, "/api/convert-project": true,
}

const maxChecklistTitle = 500

// checklistsByTask groups open and done checklist items by their to-do, in
// Things' order.
func checklistsByTask(items map[string]map[string]any) map[string][]map[string]any {
	result := map[string][]map[string]any{}
	for id, item := range items {
		title := strings.TrimSpace(asString(item, "Title"))
		taskIDs := asStringSlice(item["TaskIDs"])
		if title == "" || len(taskIDs) == 0 {
			continue
		}
		result[taskIDs[0]] = append(result[taskIDs[0]], map[string]any{
			"uuid":  id,
			"title": title,
			"done":  int(asNumber(item, "Status")) == 3,
			"index": int(asNumber(item, "Index")),
		})
	}
	for _, list := range result {
		sort.SliceStable(list, func(i, j int) bool {
			if a, b := int(asNumber(list[i], "index")), int(asNumber(list[j], "index")); a != b {
				return a < b
			}
			return asString(list[i], "uuid") < asString(list[j], "uuid")
		})
	}
	return result
}

// topPositions returns ix and ti that put a new to-do at the top of its
// list: above every to-do there.
func topPositions(when, project, area string) (int, int, bool) {
	result, err := models.cached()
	if err != nil || result.model == nil {
		return 0, 0, false
	}
	tasks, _ := result.model["tasks"].([]map[string]any)
	lists, _ := result.model["lists"].(map[string][]map[string]any)
	candidates := tasks
	switch {
	case project != "":
		candidates = filterTasks(tasks, func(task map[string]any) bool { return asString(task, "project") == project })
	case area != "":
		candidates = filterTasks(tasks, func(task map[string]any) bool { return asString(task, "area") == area })
	case lists[when] != nil:
		candidates = lists[when]
	}
	if len(candidates) == 0 {
		return 0, 0, true
	}
	index, todayIndex := 0, 0
	for i, task := range candidates {
		ix, ti := int(asNumber(task, "index")), int(asNumber(task, "todayIndex"))
		if i == 0 || ix < index {
			index = ix
		}
		if i == 0 || ti < todayIndex {
			todayIndex = ti
		}
	}
	return index - reorderStep, todayIndex - reorderStep, true
}

func filterTasks(tasks []map[string]any, keep func(map[string]any) bool) []map[string]any {
	result := []map[string]any{}
	for _, task := range tasks {
		if keep(task) {
			result = append(result, task)
		}
	}
	return result
}

// withTask returns a copy of model in which the to-do uuid is replaced by
// change(copy of the to-do). Other responses may still encode the old model.
func withTask(uuid string, change func(map[string]any)) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		var updated map[string]any
		replace := func(rows []map[string]any) []map[string]any {
			result := make([]map[string]any, len(rows))
			for i, row := range rows {
				if asString(row, "uuid") == uuid {
					if updated == nil {
						updated = maps.Clone(row)
						change(updated)
					}
					row = updated
				}
				result[i] = row
			}
			return result
		}
		tasks, _ := model["tasks"].([]map[string]any)
		next["tasks"] = replace(tasks)
		if lists, ok := model["lists"].(map[string][]map[string]any); ok {
			nextLists := make(map[string][]map[string]any, len(lists))
			for view, rows := range lists {
				nextLists[view] = replace(rows)
			}
			next["lists"] = nextLists
		}
		return next
	}
}

func cloneChecklist(task map[string]any) []map[string]any {
	items, _ := task["checklist"].([]map[string]any)
	result := make([]map[string]any, len(items))
	for i, item := range items {
		result[i] = maps.Clone(item)
	}
	return result
}

// writeBatch runs ops in one things-cli batch and returns the identifiers it
// reports per operation. edit patches the cached model; it gets them too.
func writeBatch(w http.ResponseWriter, ops []map[string]any, edit func(ids []string) func(map[string]any) map[string]any) ([]string, bool) {
	input, _ := json.Marshal(ops)
	out, err := runCLIBatch(input)
	if err != nil {
		errorResponse(w, err)
		return nil, false
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
	if edit != nil {
		models.wrote(edit(ids))
	} else {
		models.wrote(nil)
	}
	return ids, true
}

func validText(value string, max int) bool {
	return len(value) <= max && safeCLIValue(value)
}

// handleUpdate serves POST /api/update {"uuid","title"?,"note"?}.
func handleUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID  string  `json:"uuid"`
		Title *string `json:"title"`
		Note  *string `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) || (body.Title == nil && body.Note == nil) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Nothing to change."})
		return
	}
	args := []string{"edit", body.UUID}
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if title == "" || !validText(title, 500) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The title must not be empty or start with --."})
			return
		}
		body.Title = &title
		args = append(args, "--title", title)
	}
	if body.Note != nil {
		note := strings.TrimRight(*body.Note, " \t\r\n")
		if !validText(note, 5000) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The notes are too long or start with --."})
			return
		}
		body.Note = &note
		args = append(args, "--note", note)
	}
	if _, err := withWriteRetry(func() ([]byte, error) { return runCLIOutput(args...) }); err != nil {
		errorResponse(w, err)
		return
	}
	models.wrote(func(model map[string]any) map[string]any {
		model = withTask(body.UUID, func(task map[string]any) {
			if body.Title != nil {
				task["title"] = *body.Title
			}
			if body.Note != nil {
				task["note"] = *body.Note
			}
		})(model)
		// The uuid may be a project or a heading: rename it and its to-dos'
		// labels.
		model = withCollectionItem("headings", body.UUID, "headingTitle", body.Title, nil)(model)
		return withCollectionItem("projects", body.UUID, "projectTitle", body.Title, body.Note)(model)
	})
	writeJSON(w, http.StatusOK, map[string]any{"updated": true})
}

// handleChecklist serves POST /api/checklist {"action","task","uuid","title","done","order"}.
func handleChecklist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string   `json:"action"`
		Task   string   `json:"task"`
		UUID   string   `json:"uuid"`
		Title  string   `json:"title"`
		Done   bool     `json:"done"`
		Order  []string `json:"order"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if !validThingsID(body.Task) || (body.Action != "add" && !validThingsID(body.UUID)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid checklist item."})
		return
	}
	if (body.Action == "add" || body.Action == "edit") && (title == "" || !validText(title, maxChecklistTitle)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The item must not be empty or start with --."})
		return
	}
	result, err := models.cached()
	if err != nil {
		errorResponse(w, err)
		return
	}
	var task map[string]any
	tasks, _ := result.model["tasks"].([]map[string]any)
	for _, candidate := range tasks {
		if asString(candidate, "uuid") == body.Task {
			task = candidate
		}
	}
	if task == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The to-do changed. Refresh and try again."})
		return
	}
	items := cloneChecklist(task)
	find := func(uuid string) int {
		for i, item := range items {
			if asString(item, "uuid") == uuid {
				return i
			}
		}
		return -1
	}
	if body.Action != "add" && find(body.UUID) < 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The checklist changed. Refresh and try again."})
		return
	}

	var ops []map[string]any
	switch body.Action {
	case "add":
		// New items go to the end, after the last one.
		index := 0
		for i, item := range items {
			if ix := int(asNumber(item, "index")); i == 0 || ix+reorderStep > index {
				index = ix + reorderStep
			}
		}
		ops = []map[string]any{{"cmd": "checklist-add", "task": body.Task, "title": title, "index": index}}
		items = append(items, map[string]any{"title": title, "done": false, "index": index})
	case "edit":
		ops = []map[string]any{{"cmd": "checklist-edit", "uuid": body.UUID, "title": title}}
		items[find(body.UUID)]["title"] = title
	case "complete":
		ops = []map[string]any{{"cmd": "checklist-complete", "uuid": body.UUID, "done": body.Done}}
		items[find(body.UUID)]["done"] = body.Done
	case "delete":
		ops = []map[string]any{{"cmd": "checklist-delete", "uuid": body.UUID}}
		items = append(items[:find(body.UUID)], items[find(body.UUID)+1:]...)
	case "reorder":
		order := make([]reorderTask, 0, len(body.Order))
		for _, uuid := range body.Order {
			at := find(uuid)
			if at < 0 || len(body.Order) != len(items) {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "The checklist changed. Refresh and try again."})
				return
			}
			order = append(order, reorderTask{uuid: uuid, position: int(asNumber(items[at], "index"))})
		}
		planned, err := planReorder(order, body.UUID, false)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		positions := map[string]int{}
		for _, op := range planned {
			ops = append(ops, map[string]any{"cmd": "checklist-reorder", "uuid": op.UUID, "index": *op.Index})
			positions[op.UUID] = *op.Index
		}
		reordered := make([]map[string]any, 0, len(items))
		for _, uuid := range body.Order {
			item := items[find(uuid)]
			if position, ok := positions[uuid]; ok {
				item["index"] = position
			}
			reordered = append(reordered, item)
		}
		items = reordered
		if len(ops) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown checklist action."})
		return
	}
	ids, ok := writeBatch(w, ops, func(ids []string) func(map[string]any) map[string]any {
		if body.Action == "add" && len(ids) > 0 {
			items[len(items)-1]["uuid"] = ids[0]
		}
		return withTask(body.Task, func(task map[string]any) { task["checklist"] = items })
	})
	if !ok {
		return
	}
	if body.Action == "add" && len(ids) > 0 {
		body.UUID = ids[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uuid": body.UUID})
}

// handleTrash serves POST /api/trash and /api/restore {"uuids":[...]}: to-dos
// and projects go to the Things Trash, from where they can be restored. Like
// Things, a project takes its to-dos along, and restoring it brings them back.
func handleTrash(w http.ResponseWriter, r *http.Request, restore bool) {
	var body struct {
		UUIDs []string `json:"uuids"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if len(body.UUIDs) == 0 || len(body.UUIDs) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Select to-dos first."})
		return
	}
	cmd := "trash"
	if restore {
		cmd = "restore"
	}
	ops := make([]map[string]any, 0, len(body.UUIDs))
	for _, uuid := range body.UUIDs {
		if !validThingsID(uuid) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid to-do."})
			return
		}
		ops = append(ops, map[string]any{"cmd": cmd, "uuid": uuid})
	}
	ops = append(ops, projectToDoOps(body.UUIDs, cmd, restore)...)
	var edit func([]string) func(map[string]any) map[string]any
	if !restore {
		edit = func([]string) func(map[string]any) map[string]any {
			return func(model map[string]any) map[string]any {
				for _, uuid := range body.UUIDs {
					model = withoutProject(uuid)(withoutTask(uuid)(model))
				}
				return model
			}
		}
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(ops)})
}

// projectToDoOps returns the trash or restore operations for the to-dos of
// the projects among uuids: on trash those not yet in the Trash, on restore
// those in it. Without the cache, only the listed items change.
func projectToDoOps(uuids []string, cmd string, restore bool) []map[string]any {
	cache, err := readCache()
	if err != nil {
		return nil
	}
	tasks := cache.State.Tasks
	projects := map[string]bool{}
	for _, uuid := range uuids {
		if int(asNumber(tasks[uuid], "Type")) == 1 {
			projects[uuid] = true
		}
	}
	if len(projects) == 0 {
		return nil
	}
	listed := map[string]bool{}
	for _, uuid := range uuids {
		listed[uuid] = true
	}
	ids := []string{}
	for id, task := range tasks {
		if int(asNumber(task, "Type")) != 0 || listed[id] || asBool(task, "InTrash") != restore || !validThingsID(id) {
			continue
		}
		if projects[findProjectID(id, tasks)] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	ops := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, map[string]any{"cmd": cmd, "uuid": id})
	}
	return ops
}

// withoutProject returns a copy of model without the project uuid, its
// headings and its to-dos.
func withoutProject(uuid string) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		projects, _ := model["projects"].([]map[string]any)
		found := false
		for _, project := range projects {
			found = found || asString(project, "uuid") == uuid
		}
		if !found {
			return model
		}
		next := maps.Clone(model)
		keep := func(rows []map[string]any, drop func(map[string]any) bool) []map[string]any {
			result := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				if !drop(row) {
					result = append(result, row)
				}
			}
			return result
		}
		inProject := func(row map[string]any) bool { return asString(row, "project") == uuid }
		next["projects"] = keep(projects, func(row map[string]any) bool { return asString(row, "uuid") == uuid })
		if headings, ok := model["headings"].([]map[string]any); ok {
			next["headings"] = keep(headings, inProject)
		}
		tasks, _ := model["tasks"].([]map[string]any)
		next["tasks"] = keep(tasks, inProject)
		if lists, ok := model["lists"].(map[string][]map[string]any); ok {
			nextLists := make(map[string][]map[string]any, len(lists))
			for view, rows := range lists {
				nextLists[view] = keep(rows, inProject)
			}
			next["lists"] = nextLists
		}
		recountCollections(next, next["tasks"].([]map[string]any))
		return next
	}
}

// handleReopen serves POST /api/reopen {"uuid"}: a completed to-do becomes
// open again.
func handleReopen(w http.ResponseWriter, r *http.Request) {
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
	edit := func([]string) func(map[string]any) map[string]any {
		return withTask(body.UUID, func(task map[string]any) { task["status"] = "open" })
	}
	if _, ok := writeBatch(w, []map[string]any{{"cmd": "reopen", "uuid": body.UUID}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCancel serves POST /api/cancel {"uuids":[...]}: like Things, the
// to-dos are canceled (ss 2) and stay in their lists, crossed out, until
// "Log Completed"; /api/reopen undoes it.
func handleCancel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUIDs []string `json:"uuids"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	valid := len(body.UUIDs) > 0 && len(body.UUIDs) <= 100
	for _, uuid := range body.UUIDs {
		valid = valid && validThingsID(uuid)
	}
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid to-dos."})
		return
	}
	if _, ok := cachedTasks(body.UUIDs); !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The list changed. Refresh and try again."})
		return
	}
	ops := make([]map[string]any, 0, len(body.UUIDs))
	for _, uuid := range body.UUIDs {
		ops = append(ops, map[string]any{"cmd": "cancel", "uuid": uuid})
	}
	edit := func([]string) func(map[string]any) map[string]any {
		return withTasksChanged(body.UUIDs, func(task map[string]any) { task["status"] = "canceled" })
	}
	if _, ok := writeBatch(w, ops, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLogCompleted serves POST /api/log-completed: like Things' "Log
// Completed", completed to-dos move to the Logbook.
func handleLogCompleted(w http.ResponseWriter, r *http.Request) {
	edit := func([]string) func(map[string]any) map[string]any {
		return withoutDone
	}
	if _, ok := writeBatch(w, []map[string]any{{"cmd": "log-completed"}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// withoutDone returns a copy of model without completed and canceled to-dos.
func withoutDone(model map[string]any) map[string]any {
	next := maps.Clone(model)
	tasks, _ := model["tasks"].([]map[string]any)
	next["tasks"] = filterTasks(tasks, func(task map[string]any) bool { return !isDone(task) })
	if lists, ok := model["lists"].(map[string][]map[string]any); ok {
		nextLists := make(map[string][]map[string]any, len(lists))
		for view, rows := range lists {
			nextLists[view] = filterTasks(rows, func(task map[string]any) bool { return !isDone(task) })
		}
		next["lists"] = nextLists
	}
	return next
}

// newModelTask is the model entry for a to-do just created, so it can be
// opened and edited before the next sync lists it.
func newModelTask(uuid, title, note, when, scheduled, project, area string) map[string]any {
	today := time.Now().Format("2006-01-02")
	views := []string{}
	switch {
	case scheduled != "" && scheduled > today:
		views = []string{"upcoming"}
	case scheduled != "" || when == "today":
		views = []string{"today", "anytime"}
	case when == "inbox":
		views = []string{"inbox"}
	case when == "someday" && project == "":
		views = []string{"someday"}
	case when == "anytime":
		views = []string{"anytime"}
	}
	task := map[string]any{
		"uuid": uuid, "title": title, "note": note, "status": "open", "type": "task",
		"schedule": firstString(views), "scheduledDate": scheduled, "deadline": "",
		"project": project, "projectTitle": "", "heading": "", "headingTitle": "",
		"area": area, "areaTitle": "", "tags": []string{}, "views": views,
		"index": 0, "todayIndex": 0, "todayDate": "", "checklist": []map[string]any{},
	}
	if when == "today" {
		task["scheduledDate"], task["todayDate"] = today, today
	}
	if result, err := models.cached(); err == nil {
		for _, collection := range []string{"projects", "areas"} {
			items, _ := result.model[collection].([]map[string]any)
			for _, item := range items {
				switch asString(item, "uuid") {
				case project:
					if project != "" {
						task["projectTitle"] = asString(item, "title")
						if task["area"] == "" {
							task["area"] = asString(item, "area")
						}
					}
				case area:
					if area != "" {
						task["areaTitle"] = asString(item, "title")
					}
				}
			}
		}
	}
	return task
}

// withNewTask returns a copy of model with task at the top of its lists.
func withNewTask(task map[string]any) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		tasks, _ := model["tasks"].([]map[string]any)
		next["tasks"] = append([]map[string]any{task}, tasks...)
		if lists, ok := model["lists"].(map[string][]map[string]any); ok {
			nextLists := maps.Clone(lists)
			for _, view := range asStringSlice(task["views"]) {
				nextLists[view] = append([]map[string]any{task}, lists[view]...)
			}
			next["lists"] = nextLists
		}
		return next
	}
}
