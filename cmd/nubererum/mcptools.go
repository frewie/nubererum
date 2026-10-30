package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Write tools of the MCP endpoint: create_todo, update_todo, reorder_todo
// and the checklist tools. Each composes the same things-cli operations and
// cached-model patches as the web app's endpoints (main.go create, edits.go,
// details.go, reorder.go), after checking the token's projects.

const mcpMaxChecklistItems = 50

// mcpWriteTools are refused for read-only tokens and read-only instances,
// and count against the token's hourly write limit.
var mcpWriteTools = map[string]bool{
	"complete_todo": true, "create_todo": true, "update_todo": true, "reorder_todo": true, "create_project": true, "update_project": true,
	"add_checklist_items": true, "update_checklist_item": true, "remove_checklist_item": true, "reorder_checklist": true,
}

// mcpBatch runs ops in things-cli batches and patches the cached model
// (writeBatch without the HTTP response). Things Cloud takes one write per
// item and commit, so ops that share a uuid go into successive batches;
// the reported ids come back in op order. When a batch fails, the error
// names the writes that were applied, failed and skipped, and a sync is
// started so the model reflects what got through.
func mcpBatch(ops []map[string]any, edit func(ids []string) func(map[string]any) map[string]any) ([]string, error) {
	ids := make([]string, len(ops))
	rounds := mcpBatchRounds(ops)
	for r, round := range rounds {
		input, _ := json.Marshal(round.ops)
		out, err := runCLIBatch(input)
		if err != nil {
			models.wrote(nil)
			return ids, mcpBatchFailure(err, rounds, r)
		}
		var reported struct {
			Results []struct {
				UUID string `json:"uuid"`
			} `json:"results"`
		}
		_ = json.Unmarshal(out, &reported)
		for i, result := range reported.Results {
			if i < len(round.indexes) {
				ids[round.indexes[i]] = result.UUID
			}
		}
	}
	if edit != nil {
		models.wrote(edit(ids))
	} else {
		models.wrote(nil)
	}
	return ids, nil
}

// mcpRound is one things-cli batch: ops with distinct uuids, and their
// positions in the caller's list.
type mcpRound struct {
	ops     []map[string]any
	indexes []int
}

// mcpBatchRounds splits ops so no batch holds two writes for one uuid. An
// op without a uuid (checklist-add) goes into the first batch.
func mcpBatchRounds(ops []map[string]any) []mcpRound {
	rounds := []mcpRound{}
	for i, op := range ops {
		uuid := asString(op, "uuid")
		placed := false
		for r := range rounds {
			taken := false
			for _, other := range rounds[r].ops {
				taken = taken || (uuid != "" && asString(other, "uuid") == uuid)
			}
			if !taken {
				rounds[r].ops = append(rounds[r].ops, op)
				rounds[r].indexes = append(rounds[r].indexes, i)
				placed = true
				break
			}
		}
		if !placed {
			rounds = append(rounds, mcpRound{ops: []map[string]any{op}, indexes: []int{i}})
		}
	}
	return rounds
}

// mcpBatchFailure describes a failed batch round for the agent.
func mcpBatchFailure(err error, rounds []mcpRound, failed int) error {
	cmds := func(round mcpRound) string {
		names := []string{}
		for _, op := range round.ops {
			names = append(names, asString(op, "cmd"))
		}
		return strings.Join(names, ", ")
	}
	message := fmt.Sprintf("%s. Not written: %s", strings.TrimSuffix(redact(err.Error()), "."), cmds(rounds[failed]))
	applied, skipped := []string{}, []string{}
	for r, round := range rounds {
		switch {
		case r < failed:
			applied = append(applied, cmds(round))
		case r > failed:
			skipped = append(skipped, cmds(round))
		}
	}
	if len(applied) > 0 {
		message += "; already written: " + strings.Join(applied, ", ")
	}
	if len(skipped) > 0 {
		message += "; skipped: " + strings.Join(skipped, ", ")
	}
	status := http.StatusBadGateway
	var statusErr statusError
	if errors.As(err, &statusErr) {
		status = statusErr.Status
	}
	return mcpFail(status, message)
}

// mcpWhen reads a when value: a list name or a start date (YYYY-MM-DD),
// which puts the to-do in Upcoming. "inbox" is not a place for a project's
// to-do.
func mcpWhen(value string) (when, startDate string, err error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "":
		return "", "", nil
	case value == "today" || value == "anytime" || value == "someday":
		return value, "", nil
	case validDate(value):
		return "", value, nil
	}
	return "", "", mcpFail(http.StatusBadRequest, `when must be "today", "anytime", "someday" or a date YYYY-MM-DD.`)
}

// mcpTags maps tag titles to the tags of the model, case-insensitively.
// Unknown titles are created when createMissing is set; otherwise the error
// names the known tags.
func mcpTags(titles []string, createMissing bool) (ids, names []string, err error) {
	result, err := models.cached()
	if err != nil {
		return nil, nil, err
	}
	known, _ := result.model["tags"].([]map[string]any)
	ids, names = []string{}, []string{}
	for _, title := range titles {
		title = normalizeTag(title)
		if title == "" || !validText(title, 200) {
			return nil, nil, mcpFail(http.StatusBadRequest, fmt.Sprintf("Invalid tag %q.", title))
		}
		if slicesContainsFold(names, title) {
			continue
		}
		// Stored titles may start with "#" too (e.g. "#coding"): compare
		// both sides normalized, keep the stored spelling.
		found := ""
		for _, tag := range known {
			if strings.EqualFold(normalizeTag(asString(tag, "title")), title) {
				found, title = asString(tag, "uuid"), asString(tag, "title")
				break
			}
		}
		if found == "" && !createMissing {
			all := make([]string, 0, len(known))
			for _, tag := range known {
				all = append(all, asString(tag, "title"))
			}
			sort.Strings(all)
			return nil, nil, mcpFail(http.StatusBadRequest, fmt.Sprintf("Unknown tag %q. Known tags: %s. Set create_missing_tags to create it.", title, strings.Join(all, ", ")))
		}
		if found == "" {
			found, err = mcpCreateTag(title)
			if err != nil {
				return nil, nil, err
			}
		}
		ids, names = append(ids, found), append(names, title)
	}
	return ids, names, nil
}

// mcpCreateTag creates a tag like POST /api/tag-create and returns its UUID.
func mcpCreateTag(title string) (string, error) {
	out, err := withWriteRetry(func() ([]byte, error) { return runCLIOutput("create-tag", title) })
	if err != nil {
		return "", err
	}
	var created struct {
		UUID string `json:"uuid"`
	}
	_ = json.Unmarshal(out, &created)
	if created.UUID == "" {
		return "", mcpFail(http.StatusBadGateway, "Things Cloud did not report the new tag.")
	}
	item := map[string]any{"uuid": created.UUID, "title": title, "index": 0}
	models.wrote(func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		tags, _ := model["tags"].([]map[string]any)
		next["tags"] = append(append([]map[string]any{}, tags...), item)
		return next
	})
	return created.UUID, nil
}

// mcpHeading returns the heading uuid of project from the model, or an error.
func mcpHeading(model map[string]any, uuid, project string) (map[string]any, error) {
	if !validThingsID(uuid) {
		return nil, mcpFail(http.StatusBadRequest, "A valid heading UUID is required.")
	}
	headings, _ := model["headings"].([]map[string]any)
	for _, heading := range headings {
		if asString(heading, "uuid") == uuid && asString(heading, "project") == project {
			return heading, nil
		}
	}
	return nil, mcpFail(http.StatusBadRequest, "The heading is not in that project; see the headings in list_todos.")
}

// mcpProjectTasks returns the project's to-dos in Things order.
func mcpProjectTasks(model map[string]any, project string) []map[string]any {
	tasks, _ := model["tasks"].([]map[string]any)
	result := filterTasks(tasks, func(task map[string]any) bool { return asString(task, "project") == project })
	sortTasks(result, "project")
	return result
}

// mcpBottomIndex is a position after every to-do of the project.
func mcpBottomIndex(model map[string]any, project string) int {
	index, found := 0, false
	for _, task := range mcpProjectTasks(model, project) {
		if ix := int(asNumber(task, "index")); !found || ix > index {
			index, found = ix, true
		}
	}
	if !found {
		return 0
	}
	return index + reorderStep
}

// createProject serves create_project: a project in an area tagged for the
// token, like POST /api/create with type project, with optional note, tags
// and when. The new project is in the token's scope through its area.
func (s *mcpServer) createProject(arguments json.RawMessage, token mcpToken) (map[string]any, error) {
	var args struct {
		Area              string   `json:"area"`
		Title             string   `json:"title"`
		Note              string   `json:"note"`
		Tags              []string `json:"tags"`
		CreateMissingTags bool     `json:"create_missing_tags"`
		When              string   `json:"when"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpFail(http.StatusBadRequest, "Invalid arguments.")
	}
	args.Area = strings.TrimSpace(args.Area)
	title, note := strings.TrimSpace(args.Title), strings.TrimRight(args.Note, " \t\r\n")
	if !validThingsID(args.Area) {
		return nil, mcpFail(http.StatusBadRequest, "A valid area UUID is required; see list_areas.")
	}
	if title == "" || !validText(title, 500) {
		return nil, mcpFail(http.StatusBadRequest, "The title must be 1-500 characters and not start with --.")
	}
	if !validText(note, 5000) {
		return nil, mcpFail(http.StatusBadRequest, "The note must be at most 5000 characters and not start with --.")
	}
	when, startDate, err := mcpWhen(args.When)
	if err != nil {
		return nil, err
	}
	if when == "today" {
		return nil, mcpFail(http.StatusBadRequest, `when must be "anytime", "someday" or a start date YYYY-MM-DD for a project.`)
	}
	if when == "" && startDate == "" {
		when = "anytime"
	}
	result, err := models.cached()
	if err != nil {
		return nil, err
	}
	if !token.allowedAreas(result.model)[args.Area] {
		return nil, mcpFail(http.StatusForbidden, "This token may not create projects in that area; see list_areas.")
	}
	area := mcpModelAreas(result.model)[args.Area]
	tagIDs, tagNames, err := mcpTags(args.Tags, args.CreateMissingTags)
	if err != nil {
		return nil, err
	}

	op := map[string]any{"cmd": "create", "type": "project", "title": title, "area": args.Area}
	if when != "" {
		op["when"] = when
	}
	if startDate != "" {
		op["extra"] = map[string]string{"scheduled": startDate}
	}
	if note != "" {
		op["note"] = note
	}
	projectWhen := when
	if startDate != "" {
		projectWhen = "upcoming"
	}
	project := map[string]any{"title": title, "note": note, "type": "project", "count": 0, "index": 0, "area": args.Area, "when": projectWhen, "tags": tagNames}
	ids, err := mcpBatch([]map[string]any{op}, func(ids []string) func(map[string]any) map[string]any {
		if len(ids) == 0 || ids[0] == "" {
			return func(model map[string]any) map[string]any { return model }
		}
		project["uuid"] = ids[0]
		return withNewProject(project)
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 || ids[0] == "" {
		return nil, mcpFail(http.StatusBadGateway, "Things Cloud did not report the new project.")
	}
	if len(tagIDs) > 0 {
		if _, err := mcpBatch([]map[string]any{{"cmd": "tags", "uuid": ids[0], "tags": tagIDs}}, nil); err != nil {
			return nil, mcpFail(http.StatusBadGateway, fmt.Sprintf("The project %s was created, but setting its tags failed: %s", ids[0], strings.TrimSuffix(err.Error(), ".")))
		}
	}
	return map[string]any{"created": true, "project": map[string]any{
		"uuid": ids[0], "title": title, "note": note, "area": map[string]any{"uuid": args.Area, "title": asString(area, "title")},
		"when": projectWhen, "tags": tagNames, "openCount": 0,
	}}, nil
}

// withNewProject returns a copy of model with the project added, so
// list_projects shows it before the next sync.
func withNewProject(project map[string]any) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		projects, _ := model["projects"].([]map[string]any)
		next["projects"] = append(append([]map[string]any{}, projects...), project)
		return next
	}
}

// updateProject serves update_project: title and/or note of a project the
// token may access, like POST /api/update on a project.
func (s *mcpServer) updateProject(arguments json.RawMessage, token mcpToken) (map[string]any, error) {
	var args struct {
		UUID  string  `json:"uuid"`
		Title *string `json:"title"`
		Note  *string `json:"note"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpFail(http.StatusBadRequest, "Invalid arguments.")
	}
	args.UUID = strings.TrimSpace(args.UUID)
	if !validThingsID(args.UUID) || (args.Title == nil && args.Note == nil) {
		return nil, mcpFail(http.StatusBadRequest, "Give the project uuid and title and/or note.")
	}
	result, err := models.cached()
	if err != nil {
		return nil, err
	}
	if !token.allowed(result.model)[args.UUID] {
		return nil, mcpFail(http.StatusForbidden, "This token may not access that project.")
	}
	editArgs := []string{"edit", args.UUID}
	if args.Title != nil {
		title := strings.TrimSpace(*args.Title)
		if title == "" || !validText(title, 500) {
			return nil, mcpFail(http.StatusBadRequest, "The title must be 1-500 characters and not start with --.")
		}
		args.Title = &title
		editArgs = append(editArgs, "--title", title)
	}
	if args.Note != nil {
		note := strings.TrimRight(*args.Note, " \t\r\n")
		if !validText(note, 5000) {
			return nil, mcpFail(http.StatusBadRequest, "The note must be at most 5000 characters and not start with --.")
		}
		args.Note = &note
		editArgs = append(editArgs, "--note", note)
	}
	if _, err := withWriteRetry(func() ([]byte, error) { return runCLIOutput(editArgs...) }); err != nil {
		return nil, err
	}
	models.wrote(withCollectionItem("projects", args.UUID, "projectTitle", args.Title, args.Note))
	return map[string]any{"updated": true, "uuid": args.UUID, "title": args.Title, "note": args.Note}, nil
}

// createTodo serves create_todo: a to-do in an allowed project, optionally
// under a heading, with tags, when, deadline and checklist, at the top or
// bottom of the project.
func (s *mcpServer) createTodo(arguments json.RawMessage, token mcpToken) (map[string]any, error) {
	var args struct {
		Project           string   `json:"project"`
		Title             string   `json:"title"`
		Note              string   `json:"note"`
		Heading           string   `json:"heading"`
		Tags              []string `json:"tags"`
		CreateMissingTags bool     `json:"create_missing_tags"`
		When              string   `json:"when"`
		Deadline          string   `json:"deadline"`
		Checklist         []string `json:"checklist"`
		Position          string   `json:"position"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpFail(http.StatusBadRequest, "Invalid arguments.")
	}
	args.Project, args.Heading = strings.TrimSpace(args.Project), strings.TrimSpace(args.Heading)
	title, note := strings.TrimSpace(args.Title), strings.TrimRight(args.Note, " \t\r\n")
	if !validThingsID(args.Project) {
		return nil, mcpFail(http.StatusBadRequest, "A valid project UUID is required.")
	}
	if title == "" || !validText(title, 500) {
		return nil, mcpFail(http.StatusBadRequest, "The title must be 1-500 characters and not start with --.")
	}
	if !validText(note, 5000) {
		return nil, mcpFail(http.StatusBadRequest, "The note must be at most 5000 characters and not start with --.")
	}
	when, startDate, err := mcpWhen(args.When)
	if err != nil {
		return nil, err
	}
	if args.Deadline != "" && !validDate(args.Deadline) {
		return nil, mcpFail(http.StatusBadRequest, "The deadline must be YYYY-MM-DD.")
	}
	if len(args.Checklist) > mcpMaxChecklistItems {
		return nil, mcpFail(http.StatusBadRequest, fmt.Sprintf("At most %d checklist items.", mcpMaxChecklistItems))
	}
	for _, item := range args.Checklist {
		if item = strings.TrimSpace(item); item == "" || !validText(item, maxChecklistTitle) {
			return nil, mcpFail(http.StatusBadRequest, "Checklist items must be 1-500 characters and not start with --.")
		}
	}
	position := strings.ToLower(strings.TrimSpace(args.Position))
	if position == "" {
		position = "bottom"
	}
	if position != "top" && position != "bottom" {
		return nil, mcpFail(http.StatusBadRequest, `position must be "top" or "bottom".`)
	}
	result, err := models.cached()
	if err != nil {
		return nil, err
	}
	if !token.allowed(result.model)[args.Project] {
		return nil, mcpFail(http.StatusForbidden, "This token may not access that project.")
	}
	var heading map[string]any
	if args.Heading != "" {
		if heading, err = mcpHeading(result.model, args.Heading, args.Project); err != nil {
			return nil, err
		}
	}
	tagIDs, tagNames, err := mcpTags(args.Tags, args.CreateMissingTags)
	if err != nil {
		return nil, err
	}
	if when == "" && startDate == "" {
		when = "anytime"
	}

	op := map[string]any{"cmd": "create", "title": title, "project": args.Project}
	if when != "" {
		op["when"] = when
	}
	if startDate != "" {
		op["extra"] = map[string]string{"scheduled": startDate}
	}
	if note != "" {
		op["note"] = note
	}
	if heading != nil {
		op["heading"] = args.Heading
	}
	var task map[string]any
	ids, err := mcpBatch([]map[string]any{op}, func(ids []string) func(map[string]any) map[string]any {
		if len(ids) == 0 || ids[0] == "" {
			return func(model map[string]any) map[string]any { return model }
		}
		task = newModelTask(ids[0], title, note, when, startDate, args.Project, "")
		if heading != nil {
			task["heading"], task["headingTitle"] = args.Heading, asString(heading, "title")
		}
		return withNewTask(task)
	})
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, mcpFail(http.StatusBadGateway, "Things Cloud did not report the new to-do.")
	}
	uuid := ids[0]

	// Tags, deadline, checklist and position need the UUID: a second batch.
	extras := []map[string]any{}
	if len(tagIDs) > 0 {
		extras = append(extras, map[string]any{"cmd": "tags", "uuid": uuid, "tags": tagIDs})
	}
	if args.Deadline != "" {
		extras = append(extras, map[string]any{"cmd": "deadline", "uuid": uuid, "deadlineDate": args.Deadline})
	}
	checklist := []map[string]any{}
	checklistAt := []int{}
	for i, item := range args.Checklist {
		item = strings.TrimSpace(item)
		checklistAt = append(checklistAt, len(extras))
		extras = append(extras, map[string]any{"cmd": "checklist-add", "task": uuid, "title": item, "index": i * reorderStep})
		checklist = append(checklist, map[string]any{"title": item, "done": false, "index": i * reorderStep})
	}
	index := mcpBottomIndex(result.model, args.Project)
	if position == "top" {
		if top, _, ok := topPositions(when, args.Project, ""); ok {
			index = top
		}
	}
	extras = append(extras, map[string]any{"cmd": "reorder", "uuid": uuid, "index": index})
	_, err = mcpBatch(extras, func(ids []string) func(map[string]any) map[string]any {
		for i, at := range checklistAt {
			checklist[i]["uuid"] = ids[at]
		}
		return withTask(uuid, func(task map[string]any) {
			task["tags"], task["tagIDs"] = tagNames, tagIDs
			task["deadline"] = args.Deadline
			task["checklist"] = checklist
			task["index"] = index
		})
	})
	if err != nil {
		return nil, mcpFail(http.StatusBadGateway, fmt.Sprintf("The to-do %s was created with title and note, but: %s. Set the rest with update_todo and add_checklist_items.", uuid, strings.TrimSuffix(err.Error(), ".")))
	}
	task = maps.Clone(task)
	task["tags"], task["deadline"], task["checklist"], task["index"] = tagNames, args.Deadline, checklist, index
	return map[string]any{"created": true, "todo": mcpTodo(task)}, nil
}

// updateTodo serves update_todo: title, note, tags, when, deadline and
// heading of a to-do in an allowed project. Only the given fields change.
func (s *mcpServer) updateTodo(arguments json.RawMessage, token mcpToken) (map[string]any, error) {
	var args struct {
		UUID              string    `json:"uuid"`
		Title             *string   `json:"title"`
		Note              *string   `json:"note"`
		Tags              *[]string `json:"tags"`
		CreateMissingTags bool      `json:"create_missing_tags"`
		When              *string   `json:"when"`
		Deadline          *string   `json:"deadline"`
		Heading           *string   `json:"heading"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpFail(http.StatusBadRequest, "Invalid arguments.")
	}
	args.UUID = strings.TrimSpace(args.UUID)
	if !validThingsID(args.UUID) {
		return nil, mcpFail(http.StatusBadRequest, "A valid to-do UUID is required.")
	}
	if args.Title == nil && args.Note == nil && args.Tags == nil && args.When == nil && args.Deadline == nil && args.Heading == nil {
		return nil, mcpFail(http.StatusBadRequest, "Nothing to change: give title, note, tags, when, deadline or heading.")
	}
	task, err := s.allowedTask(args.UUID, token)
	if err != nil {
		return nil, err
	}
	project := asString(task, "project")

	editArgs := []string{"edit", args.UUID}
	if args.Title != nil {
		title := strings.TrimSpace(*args.Title)
		if title == "" || !validText(title, 500) {
			return nil, mcpFail(http.StatusBadRequest, "The title must be 1-500 characters and not start with --.")
		}
		args.Title = &title
		editArgs = append(editArgs, "--title", title)
	}
	if args.Note != nil {
		note := strings.TrimRight(*args.Note, " \t\r\n")
		if !validText(note, 5000) {
			return nil, mcpFail(http.StatusBadRequest, "The note must be at most 5000 characters and not start with --.")
		}
		args.Note = &note
		editArgs = append(editArgs, "--note", note)
	}
	var when, startDate string
	if args.When != nil {
		if when, startDate, err = mcpWhen(*args.When); err != nil {
			return nil, err
		}
		if when == "" && startDate == "" {
			return nil, mcpFail(http.StatusBadRequest, `when must be "today", "anytime", "someday" or a date YYYY-MM-DD.`)
		}
	}
	if args.Deadline != nil && *args.Deadline != "" && !validDate(*args.Deadline) {
		return nil, mcpFail(http.StatusBadRequest, "The deadline must be YYYY-MM-DD or empty to remove it.")
	}
	var heading map[string]any
	if args.Heading != nil && *args.Heading != "" {
		result, err := models.cached()
		if err != nil {
			return nil, err
		}
		if heading, err = mcpHeading(result.model, strings.TrimSpace(*args.Heading), project); err != nil {
			return nil, err
		}
	}
	var tagIDs, tagNames []string
	if args.Tags != nil {
		if tagIDs, tagNames, err = mcpTags(*args.Tags, args.CreateMissingTags); err != nil {
			return nil, err
		}
	}

	if len(editArgs) > 2 {
		if _, err := withWriteRetry(func() ([]byte, error) { return runCLIOutput(editArgs...) }); err != nil {
			return nil, err
		}
		models.wrote(withTask(args.UUID, func(task map[string]any) {
			if args.Title != nil {
				task["title"] = *args.Title
			}
			if args.Note != nil {
				task["note"] = *args.Note
			}
		}))
	}
	ops := []map[string]any{}
	if args.Tags != nil {
		ops = append(ops, map[string]any{"cmd": "tags", "uuid": args.UUID, "tags": tagIDs})
	}
	if args.When != nil {
		op := map[string]any{"cmd": "schedule", "uuid": args.UUID}
		if when != "" {
			op["when"] = when
		} else {
			op["startDate"] = startDate
		}
		ops = append(ops, op)
	}
	if args.Deadline != nil {
		ops = append(ops, map[string]any{"cmd": "deadline", "uuid": args.UUID, "deadlineDate": *args.Deadline})
	}
	if args.Heading != nil {
		switch {
		case heading != nil && asString(task, "heading") != asString(heading, "uuid"):
			ops = append(ops, map[string]any{"cmd": "move", "uuid": args.UUID, "heading": asString(heading, "uuid")})
		case heading == nil && asString(task, "heading") != "":
			ops = append(ops, map[string]any{"cmd": "move", "uuid": args.UUID, "project": project})
		}
	}
	if len(ops) > 0 {
		today := time.Now().Format("2006-01-02")
		_, err = mcpBatch(ops, func([]string) func(map[string]any) map[string]any {
			return withTask(args.UUID, func(task map[string]any) {
				if args.Tags != nil {
					task["tags"], task["tagIDs"] = tagNames, tagIDs
				}
				if args.When != nil {
					task["views"] = scheduledViews(when, startDate, task)
					task["schedule"] = firstString(asStringSlice(task["views"]))
					switch {
					case startDate != "":
						task["scheduledDate"], task["todayDate"] = startDate, startDate
					case when == "today":
						task["scheduledDate"], task["todayDate"] = today, today
					default:
						task["scheduledDate"], task["todayDate"] = "", ""
					}
				}
				if args.Deadline != nil {
					task["deadline"] = *args.Deadline
				}
				if args.Heading != nil {
					task["heading"], task["headingTitle"] = "", ""
					if heading != nil {
						task["heading"], task["headingTitle"] = asString(heading, "uuid"), asString(heading, "title")
					}
				}
			})
		})
		if err != nil {
			return nil, err
		}
	}
	updated := mcpCurrentTask(args.UUID, task)
	return map[string]any{"updated": true, "todo": mcpTodo(updated)}, nil
}

// reorderTodo serves reorder_todo: moves a to-do before or after another
// to-do of the same project (into that one's heading section), or to the
// top or bottom of its section.
func (s *mcpServer) reorderTodo(arguments json.RawMessage, token mcpToken) (map[string]any, error) {
	var args struct {
		UUID     string `json:"uuid"`
		Before   string `json:"before"`
		After    string `json:"after"`
		Position string `json:"position"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpFail(http.StatusBadRequest, "Invalid arguments.")
	}
	args.UUID, args.Before, args.After = strings.TrimSpace(args.UUID), strings.TrimSpace(args.Before), strings.TrimSpace(args.After)
	position := strings.ToLower(strings.TrimSpace(args.Position))
	targets := 0
	for _, value := range []string{args.Before, args.After, position} {
		if value != "" {
			targets++
		}
	}
	if !validThingsID(args.UUID) || targets != 1 || (position != "" && position != "top" && position != "bottom") {
		return nil, mcpFail(http.StatusBadRequest, `Give the to-do uuid and exactly one of before, after or position ("top"/"bottom").`)
	}
	task, err := s.allowedTask(args.UUID, token)
	if err != nil {
		return nil, err
	}
	project, heading := asString(task, "project"), asString(task, "heading")
	neighbour := args.Before
	if neighbour == "" {
		neighbour = args.After
	}
	if neighbour != "" {
		if neighbour == args.UUID {
			return nil, mcpFail(http.StatusBadRequest, "A to-do cannot be placed next to itself.")
		}
		other, err := s.allowedTask(neighbour, token)
		if err != nil {
			return nil, err
		}
		if asString(other, "project") != project {
			return nil, mcpFail(http.StatusBadRequest, "Both to-dos must be in the same project.")
		}
		heading = asString(other, "heading")
	}
	result, err := models.cached()
	if err != nil {
		return nil, err
	}
	section := filterTasks(mcpProjectTasks(result.model, project), func(row map[string]any) bool {
		return asString(row, "heading") == heading && asString(row, "uuid") != args.UUID
	})
	moved := reorderTask{uuid: args.UUID, position: int(asNumber(task, "index"))}
	order := make([]reorderTask, 0, len(section)+1)
	at := -1
	for i, row := range section {
		uuid := asString(row, "uuid")
		if uuid == args.Before {
			at = i
		}
		order = append(order, reorderTask{uuid: uuid, position: int(asNumber(row, "index"))})
		if uuid == args.After {
			at = i + 1
		}
	}
	switch {
	case position == "top":
		at = 0
	case position == "bottom":
		at = len(order)
	case at < 0:
		return nil, mcpFail(http.StatusConflict, "The other to-do is no longer in that section. Refresh with list_todos.")
	}
	order = append(order[:at], append([]reorderTask{moved}, order[at:]...)...)
	planned, err := planReorder(order, args.UUID, false)
	if err != nil {
		return nil, mcpFail(http.StatusBadRequest, err.Error())
	}
	ops := []map[string]any{}
	headingTitle := ""
	if heading != asString(task, "heading") {
		if heading == "" {
			ops = append(ops, map[string]any{"cmd": "move", "uuid": args.UUID, "project": project})
		} else {
			ops = append(ops, map[string]any{"cmd": "move", "uuid": args.UUID, "heading": heading})
			if entry, err := mcpHeading(result.model, heading, project); err == nil {
				headingTitle = asString(entry, "title")
			}
		}
	}
	for _, op := range planned {
		ops = append(ops, map[string]any{"cmd": "reorder", "uuid": op.UUID, "index": *op.Index})
	}
	if len(ops) == 0 {
		return map[string]any{"reordered": true, "writes": 0, "todo": mcpTodo(task)}, nil
	}
	_, err = mcpBatch(ops, func([]string) func(map[string]any) map[string]any {
		return func(model map[string]any) map[string]any {
			model = withPositions("tasks", planned)(model)
			return withTask(args.UUID, func(row map[string]any) {
				row["heading"], row["headingTitle"] = heading, headingTitle
				for _, op := range planned {
					if op.UUID == args.UUID {
						row["index"] = *op.Index
					}
				}
			})(model)
		}
	})
	if err != nil {
		return nil, err
	}
	updated := mcpCurrentTask(args.UUID, task)
	return map[string]any{"reordered": true, "writes": len(ops), "todo": mcpTodo(updated)}, nil
}

// checklistTool serves add_checklist_items, update_checklist_item,
// remove_checklist_item and reorder_checklist on a to-do in an allowed
// project. Checklist items are the subtasks of a Things to-do.
func (s *mcpServer) checklistTool(name string, arguments json.RawMessage, token mcpToken) (map[string]any, error) {
	var args struct {
		Todo   string   `json:"todo"`
		Item   string   `json:"item"`
		Titles []string `json:"titles"`
		Title  *string  `json:"title"`
		Done   *bool    `json:"done"`
		Order  []string `json:"order"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpFail(http.StatusBadRequest, "Invalid arguments.")
	}
	args.Todo, args.Item = strings.TrimSpace(args.Todo), strings.TrimSpace(args.Item)
	if !validThingsID(args.Todo) {
		return nil, mcpFail(http.StatusBadRequest, "A valid to-do UUID (todo) is required.")
	}
	task, err := s.allowedTask(args.Todo, token)
	if err != nil {
		return nil, err
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
	if name != "add_checklist_items" && name != "reorder_checklist" && (!validThingsID(args.Item) || find(args.Item) < 0) {
		return nil, mcpFail(http.StatusBadRequest, "Unknown checklist item; see the checklist in list_todos.")
	}

	ops := []map[string]any{}
	added := 0
	switch name {
	case "add_checklist_items":
		if len(args.Titles) == 0 || len(args.Titles) > mcpMaxChecklistItems {
			return nil, mcpFail(http.StatusBadRequest, fmt.Sprintf("Give 1-%d titles.", mcpMaxChecklistItems))
		}
		index := 0
		for i, item := range items {
			if ix := int(asNumber(item, "index")); i == 0 || ix+reorderStep > index {
				index = ix + reorderStep
			}
		}
		for _, title := range args.Titles {
			title = strings.TrimSpace(title)
			if title == "" || !validText(title, maxChecklistTitle) {
				return nil, mcpFail(http.StatusBadRequest, "Checklist items must be 1-500 characters and not start with --.")
			}
			ops = append(ops, map[string]any{"cmd": "checklist-add", "task": args.Todo, "title": title, "index": index})
			items = append(items, map[string]any{"title": title, "done": false, "index": index})
			index += reorderStep
			added++
		}
	case "update_checklist_item":
		if args.Title == nil && args.Done == nil {
			return nil, mcpFail(http.StatusBadRequest, "Give title and/or done.")
		}
		at := find(args.Item)
		if args.Title != nil {
			title := strings.TrimSpace(*args.Title)
			if title == "" || !validText(title, maxChecklistTitle) {
				return nil, mcpFail(http.StatusBadRequest, "The item must be 1-500 characters and not start with --.")
			}
			ops = append(ops, map[string]any{"cmd": "checklist-edit", "uuid": args.Item, "title": title})
			items[at]["title"] = title
		}
		if args.Done != nil {
			ops = append(ops, map[string]any{"cmd": "checklist-complete", "uuid": args.Item, "done": *args.Done})
			items[at]["done"] = *args.Done
		}
	case "remove_checklist_item":
		at := find(args.Item)
		ops = append(ops, map[string]any{"cmd": "checklist-delete", "uuid": args.Item})
		items = append(items[:at], items[at+1:]...)
	case "reorder_checklist":
		if len(args.Order) != len(items) {
			return nil, mcpFail(http.StatusBadRequest, "order must list every checklist item once.")
		}
		reordered := make([]map[string]any, 0, len(items))
		seen := map[string]bool{}
		for i, uuid := range args.Order {
			at := find(uuid)
			if at < 0 || seen[uuid] {
				return nil, mcpFail(http.StatusBadRequest, "order must list every checklist item once.")
			}
			seen[uuid] = true
			item := items[at]
			if position := i * reorderStep; int(asNumber(item, "index")) != position {
				ops = append(ops, map[string]any{"cmd": "checklist-reorder", "uuid": uuid, "index": position})
				item["index"] = position
			}
			reordered = append(reordered, item)
		}
		items = reordered
	}
	if len(ops) > 0 {
		if _, err := mcpBatch(ops, func(ids []string) func(map[string]any) map[string]any {
			for i := 0; i < added && i < len(ids); i++ {
				items[len(items)-added+i]["uuid"] = ids[i]
			}
			return withTask(args.Todo, func(task map[string]any) { task["checklist"] = items })
		}); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "writes": len(ops), "todo": args.Todo, "checklist": mcpChecklist(items)}, nil
}

// mcpCurrentTask returns the to-do as the cached model has it now, or
// fallback when it is gone.
func mcpCurrentTask(uuid string, fallback map[string]any) map[string]any {
	if result, err := models.cached(); err == nil {
		if task := mcpFindTask(result.model, uuid); task != nil {
			return task
		}
	}
	return fallback
}

func mcpChecklist(items []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{"uuid": asString(item, "uuid"), "title": asString(item, "title"), "done": asBool(item, "done")})
	}
	return result
}

// mcpWriteToolDefinitions are the tools offered to tokens that may write.
func mcpWriteToolDefinitions() []map[string]any {
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	strings := func(description string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
	}
	object := func(properties map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	whenDescription := `"today", "anytime", "someday" or a start date YYYY-MM-DD (Upcoming).`
	return []map[string]any{
		{
			"name":        "create_project",
			"description": "Creates a project in an area this token may create projects in (see list_areas). Optional: note, tags (titles; unknown ones need create_missing_tags), when (\"anytime\" default, \"someday\" or a start date). The new project is in this token's scope through its area.",
			"inputSchema": object(map[string]any{
				"area":                str("Area UUID from list_areas."),
				"title":               str("Project title, 1-500 characters."),
				"note":                str("Project notes, up to 5000 characters."),
				"tags":                strings("Tag titles for the project. See list_tags."),
				"create_missing_tags": map[string]any{"type": "boolean", "description": "Create tags that do not exist yet.", "default": false},
				"when":                str(`"anytime" (default), "someday" or a start date YYYY-MM-DD.`),
			}, "area", "title"),
		},
		{
			"name":        "update_project",
			"description": "Renames a project this token may access and/or replaces its notes.",
			"inputSchema": object(map[string]any{"uuid": str("Project UUID from list_projects."), "title": str("New title."), "note": str("New notes (replace the existing ones).")}, "uuid"),
		},
		{
			"name":        "complete_todo",
			"description": "Marks one to-do as completed, like ticking it off in Things. The to-do must belong to a project this token may access. Already completed to-dos are left as they are.",
			"inputSchema": object(map[string]any{"uuid": str("To-do UUID from list_todos.")}, "uuid"),
		},
		{
			"name":        "create_todo",
			"description": "Creates a to-do in a project this token may access, at the bottom of the project (or top). Optional: note, heading (UUID from list_todos), tags (titles; unknown ones need create_missing_tags), when, deadline, checklist (subtask titles in order).",
			"inputSchema": object(map[string]any{
				"project":             str("Project UUID from list_projects."),
				"title":               str("Title, 1-500 characters."),
				"note":                str("Notes, up to 5000 characters; plain text or Markdown."),
				"heading":             str("Heading UUID from list_todos to file the to-do under."),
				"tags":                strings("Tag titles, e.g. [\"Coding\"]. See list_tags."),
				"create_missing_tags": map[string]any{"type": "boolean", "description": "Create tags that do not exist yet.", "default": false},
				"when":                str(whenDescription + " Default: anytime."),
				"deadline":            str("Deadline YYYY-MM-DD."),
				"checklist":           strings("Checklist items (subtasks), top to bottom."),
				"position":            map[string]any{"type": "string", "enum": []string{"top", "bottom"}, "description": "Where in the project the to-do goes. Default: bottom.", "default": "bottom"},
			}, "project", "title"),
		},
		{
			"name":        "update_todo",
			"description": "Changes a to-do in a project this token may access. Only the fields given change: title, note (replaces the notes), tags (replaces all tags), when, deadline (\"\" removes it), heading (UUID; \"\" moves it out of its heading).",
			"inputSchema": object(map[string]any{
				"uuid":                str("To-do UUID from list_todos."),
				"title":               str("New title."),
				"note":                str("New notes (replaces the existing ones)."),
				"tags":                strings("New tag titles (replace the existing ones). See list_tags."),
				"create_missing_tags": map[string]any{"type": "boolean", "description": "Create tags that do not exist yet.", "default": false},
				"when":                str(whenDescription),
				"deadline":            str("Deadline YYYY-MM-DD, or \"\" to remove it."),
				"heading":             str("Heading UUID in the same project, or \"\" for none."),
			}, "uuid"),
		},
		{
			"name":        "reorder_todo",
			"description": "Moves a to-do within its project: before or after another to-do of the project (joining that one's heading), or to the top or bottom of its heading section. Give exactly one of before, after, position.",
			"inputSchema": object(map[string]any{
				"uuid":     str("To-do UUID to move."),
				"before":   str("UUID of the to-do it goes in front of."),
				"after":    str("UUID of the to-do it goes behind."),
				"position": map[string]any{"type": "string", "enum": []string{"top", "bottom"}, "description": "Top or bottom of its section."},
			}, "uuid"),
		},
		{
			"name":        "add_checklist_items",
			"description": "Appends checklist items (subtasks) to a to-do, in the given order.",
			"inputSchema": object(map[string]any{"todo": str("To-do UUID."), "titles": strings("Item titles, 1-500 characters each.")}, "todo", "titles"),
		},
		{
			"name":        "update_checklist_item",
			"description": "Renames a checklist item and/or ticks it off (done true) or reopens it (done false).",
			"inputSchema": object(map[string]any{"todo": str("To-do UUID."), "item": str("Checklist item UUID from list_todos."), "title": str("New title."), "done": map[string]any{"type": "boolean", "description": "true = done, false = open."}}, "todo", "item"),
		},
		{
			"name":        "remove_checklist_item",
			"description": "Deletes a checklist item from a to-do.",
			"inputSchema": object(map[string]any{"todo": str("To-do UUID."), "item": str("Checklist item UUID.")}, "todo", "item"),
		},
		{
			"name":        "reorder_checklist",
			"description": "Sets the order of a to-do's checklist: every item UUID once, top to bottom.",
			"inputSchema": object(map[string]any{"todo": str("To-do UUID."), "order": strings("All checklist item UUIDs in the new order.")}, "todo", "order"),
		},
	}
}
