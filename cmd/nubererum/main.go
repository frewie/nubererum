package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"maps"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type sessionState struct {
	Email    string
	Password string
}

var session = sessionState{
	Email:    firstEnv("THINGS_USERNAME", "THINGS_EMAIL"),
	Password: firstEnv("THINGS_PASSWORD", "THINGS_TOKEN", "THINGS_CLOUD_PASSWORD"),
}

var views = []string{"today", "inbox", "anytime", "upcoming", "someday"}
var sessionMu sync.RWMutex
var (
	noteBreakRE   = regexp.MustCompile(`(?i)<br\s*/?>`)
	noteWrapperRE = regexp.MustCompile(`(?is)</?note\b[^>]*>`)
	xmlTagRE      = regexp.MustCompile(`(?is)<[^>]+>`)
)

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func appRoot() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		if exists(filepath.Join(dir, "public", "index.html")) {
			return dir
		}
	}
	wd, err := os.Getwd()
	if err == nil {
		return wd
	}
	return "."
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cliPath() string {
	root := appRoot()
	candidates := []string{
		os.Getenv("THINGS_CLOUD_CLI"),
		filepath.Join(root, "bin", "things-cli"),
		"/usr/local/bin/things-cli",
		"things-cli",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, string(os.PathSeparator)) {
			if exists(candidate) {
				return candidate
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.Header().Set("cache-control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1_000_000))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	return json.Unmarshal(body, target)
}

func runCLI(args ...string) ([]map[string]any, error) {
	sessionMu.RLock()
	email := session.Email
	password := session.Password
	sessionMu.RUnlock()
	return runCLIWithCredentials(email, password, args...)
}

func runCLIWithCredentials(email, password string, args ...string) ([]map[string]any, error) {
	out, err := runCLIOutputWithCredentials(email, password, args...)
	if err != nil {
		return nil, err
	}
	return parseCLIRows(out)
}

func parseCLIRows(out []byte) ([]map[string]any, error) {
	if len(bytes.TrimSpace(out)) == 0 {
		return []map[string]any{}, nil
	}
	var items []map[string]any
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, statusError{Status: http.StatusBadGateway, Message: "things-cli returned non-JSON output: " + string(out)}
	}
	return items, nil
}

func runCLIOutput(args ...string) ([]byte, error) {
	sessionMu.RLock()
	email := session.Email
	password := session.Password
	sessionMu.RUnlock()
	return runCLIOutputWithCredentials(email, password, args...)
}

func runCLIOutputWithCredentials(email, password string, args ...string) ([]byte, error) {
	return execCLI(email, password, nil, false, args...)
}

// readWithRetry runs a read command and retries it once after a failed name
// lookup or refused connection.
func readWithRetry(args ...string) ([]map[string]any, error) {
	rows, err := runCLI(args...)
	if retryable(err) {
		time.Sleep(writeRetryDelays[0])
		rows, err = runCLI(args...)
	}
	return rows, err
}

// readOffline runs a read command on the state of the previous command when
// the worker runs, and like readWithRetry otherwise.
func readOffline(args ...string) ([]map[string]any, error) {
	sessionMu.RLock()
	email, password := session.Email, session.Password
	sessionMu.RUnlock()
	out, err := execCLI(email, password, nil, true, args...)
	if retryable(err) {
		time.Sleep(writeRetryDelays[0])
		out, err = execCLI(email, password, nil, true, args...)
	}
	if err != nil {
		return nil, err
	}
	return parseCLIRows(out)
}

// moveInBackground runs the reorder that follows a create. The cached model
// already shows the new position; a sync follows the write.
func moveInBackground(input []byte, failure, uuid string) {
	if _, err := runCLIBatch(input); err != nil {
		logError(failure, err, "uuid", uuid)
		return
	}
	models.wrote(nil)
}

// cliEnv passes the account to things-cli through its environment, never
// through arguments.
func cliEnv(email, password string) []string {
	return append(os.Environ(),
		"THINGS_USERNAME="+email,
		"THINGS_PASSWORD="+password,
		"THINGS_TOKEN="+password,
		"THINGS_CLI_CACHE="+cachePath(),
	)
}

type statusError struct {
	Status  int
	Message string
}

func (e statusError) Error() string {
	return e.Message
}

func errorResponse(w http.ResponseWriter, err error) {
	var statusErr statusError
	if errors.As(err, &statusErr) {
		writeJSON(w, statusErr.Status, map[string]any{"error": statusErr.Message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func backendInfo() map[string]any {
	cli := cliPath()
	mode := "missing-cli"
	if cli != "" {
		mode = "things-cli"
	}
	return map[string]any{
		"mode":           mode,
		"cli":            cliName(cli),
		"nativeFallback": false,
		"directCloud":    true,
	}
}

// cliName is the file name of the CLI, or "" when there is none.
func cliName(cli string) string {
	if cli == "" {
		return ""
	}
	return filepath.Base(cli)
}

func cachePath() string {
	if configured := os.Getenv("THINGS_CLI_CACHE"); configured != "" {
		return configured
	}
	dataDir := firstEnv("DATA_DIR")
	if dataDir == "" {
		dataDir = appRoot()
	}
	_ = os.MkdirAll(dataDir, 0o700)
	return filepath.Join(dataDir, "things-cli-state-v8.json")
}

func asString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func asBool(item map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			switch typed := value.(type) {
			case bool:
				return typed
			case string:
				return typed == "true"
			}
		}
	}
	return false
}

func asStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := fmt.Sprint(item); text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
		return result
	default:
		return []string{}
	}
}

func asNumber(item map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			switch typed := value.(type) {
			case float64:
				return typed
			case int:
				return float64(typed)
			case json.Number:
				n, _ := typed.Float64()
				return n
			}
		}
	}
	return 0
}

func cleanNote(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	text = noteBreakRE.ReplaceAllString(text, "\n")
	text = noteWrapperRE.ReplaceAllString(text, "")
	text = xmlTagRE.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

func normalizeCollection(items []map[string]any, itemType string) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		title := asString(item, "title", "name")
		if title == "" {
			title = "Untitled"
		}
		result = append(result, map[string]any{
			"uuid":  asString(item, "uuid", "id"),
			"title": title,
			"type":  itemType,
			"count": asNumber(item, "count", "openTasks"),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return fmt.Sprint(result[i]["title"]) < fmt.Sprint(result[j]["title"])
	})
	return result
}

type cachedStateFile struct {
	HistoryID   string `json:"historyId"`
	ServerIndex int    `json:"serverIndex"`
	State       struct {
		Areas          map[string]map[string]any `json:"Areas"`
		Tasks          map[string]map[string]any `json:"Tasks"`
		Tags           map[string]map[string]any `json:"Tags"`
		CheckListItems map[string]map[string]any `json:"CheckListItems"`
		LogDate        string                    `json:"LogDate"`
		LogInterval    *int                      `json:"LogInterval"`
	} `json:"state"`
}

// syncModel builds the model from the CLI cache. `things-cli list` lists the
// open tasks (it skips completed, canceled and trashed ones), which limits the
// model to visible tasks; `things-cli today` decides Today membership.
func syncModel() (map[string]any, error) {
	listRows, err := readWithRetry("list")
	if err != nil {
		return nil, err
	}
	// Right after list, today needs no new data from Things Cloud.
	todayRows, err := readOffline("today")
	if err != nil {
		return nil, err
	}
	model, err := syncModelFromCache()
	if err != nil {
		return nil, err
	}
	if len(listRows) > 0 {
		applyVisibleTaskFilter(model, listRows)
	}
	applyCLIToday(model, todayRows)
	return model, nil
}

func appendUnique(value any, view string) []string {
	result := []string{}
	switch typed := value.(type) {
	case []string:
		result = append(result, typed...)
	case []any:
		for _, item := range typed {
			result = append(result, fmt.Sprint(item))
		}
	}
	for _, existing := range result {
		if existing == view {
			return result
		}
	}
	return append(result, view)
}

func syncModelFromCache() (map[string]any, error) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil, err
	}
	var cache cachedStateFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	today := time.Now().Format("2006-01-02")
	projectsByID := map[string]map[string]any{}
	areasByID := map[string]map[string]any{}
	tagsByID := map[string]map[string]any{}

	for id, tag := range cache.State.Tags {
		if title := asString(tag, "Title", "title", "name"); title != "" {
			tagsByID[id] = map[string]any{"uuid": id, "title": title, "index": int(asNumber(tag, "Index"))}
		}
	}
	for id, area := range cache.State.Areas {
		title := asString(area, "Title", "title", "name")
		if title == "" {
			continue
		}
		tagIDs, tagTitles := areaTags(area, tagsByID)
		areasByID[id] = map[string]any{
			"uuid":   asString(area, "UUID", "uuid", "id"),
			"title":  title,
			"type":   "area",
			"count":  0,
			"index":  int(asNumber(area, "Index")),
			"tags":   tagTitles,
			"tagIDs": tagIDs,
		}
	}
	logbook := newLogbookRule(cache.State.LogDate, cache.State.LogInterval, time.Now())
	progress := projectProgress(cache.State.Tasks)
	for id, task := range cache.State.Tasks {
		// Completed and canceled projects stay, like to-dos, until logged.
		if !isOpenCacheItem(task, 1) && !logbook.stillListedAs(task, 1) {
			continue
		}
		title := asString(task, "Title", "title", "name")
		if title == "" {
			continue
		}
		projectTags := []string{}
		for _, tagID := range asStringSlice(task["TagIDs"]) {
			if tag, ok := tagsByID[tagID]; ok {
				projectTags = append(projectTags, fmt.Sprint(tag["title"]))
			}
		}
		projectsByID[id] = map[string]any{
			"uuid":  asString(task, "UUID", "uuid", "id"),
			"title": title,
			"note":  asString(task, "Note"),
			"type":  "project",
			"count": 0,
			"index": int(asNumber(task, "Index")),
			"area":  firstString(asStringSlice(task["AreaIDs"])),
			"when":  projectWhen(task, today),
			"tags":  projectTags,
			// tagIDs, deadline, scheduledDate, status and the progress
			// (done of total to-dos) feed the project header and the pie.
			"tagIDs":        taskTagIDs(task, tagsByID),
			"deadline":      deadlineDate(asString(task, "DeadlineDate")),
			"scheduledDate": dateOnly(asString(task, "ScheduledDate")),
			"status":        projectStatus(task),
			"done":          progress[id][0],
			"total":         progress[id][1],
		}
	}

	checklists := checklistsByTask(cache.State.CheckListItems)

	tasks := []map[string]any{}
	for id, task := range cache.State.Tasks {
		open := isOpenCacheItem(task, 0)
		if !open && !logbook.stillListed(task) {
			continue
		}
		title := strings.TrimSpace(asString(task, "Title", "title", "name"))
		if title == "" {
			continue
		}

		projectID := findProjectID(id, cache.State.Tasks)
		headingID := findHeadingID(id, cache.State.Tasks)
		var project map[string]any
		if projectID != "" {
			project = cache.State.Tasks[projectID]
			if _, listed := projectsByID[projectID]; !listed {
				continue
			}
		}
		if headingID != "" && !isOpenCacheItem(cache.State.Tasks[headingID], 2) {
			continue
		}
		areaID := firstString(asStringSlice(task["AreaIDs"]))
		if areaID == "" && projectID != "" {
			areaID = firstString(asStringSlice(cache.State.Tasks[projectID]["AreaIDs"]))
		}
		tagTitles := []string{}
		for _, tagID := range asStringSlice(task["TagIDs"]) {
			if tag, ok := tagsByID[tagID]; ok {
				tagTitles = append(tagTitles, fmt.Sprint(tag["title"]))
			}
		}

		viewsForTask := stateViews(task, project, today)
		status := "open"
		if !open {
			// Completed to-dos stay where they were until they are logged,
			// Today included (things-cli today lists open to-dos only).
			status = "completed"
			if int(asNumber(task, "Status")) == 2 {
				status = "canceled"
			}
			if inTodayByDate(task, today) {
				viewsForTask = append(viewsForTask, "today")
			}
		}
		modelTask := map[string]any{
			"uuid":             asString(task, "UUID", "uuid", "id"),
			"title":            title,
			"note":             cleanNote(asString(task, "Note", "note", "notes")),
			"status":           status,
			"type":             "task",
			"schedule":         firstString(viewsForTask),
			"scheduledDate":    dateOnly(asString(task, "ScheduledDate", "scheduledDate", "scheduled")),
			"deadline":         deadlineDate(asString(task, "DeadlineDate", "deadlineDate", "deadline")),
			"modificationDate": dateOnly(asString(task, "ModificationDate", "modificationDate", "modified")),
			"project":          projectID,
			"projectTitle":     titleFor(projectsByID, projectID),
			"heading":          headingID,
			"headingTitle":     titleForTask(cache.State.Tasks, headingID),
			"area":             areaID,
			"areaTitle":        titleFor(areasByID, areaID),
			"tags":             tagTitles,
			"tagIDs":           taskTagIDs(task, tagsByID),
			"views":            viewsForTask,
			"index":            int(asNumber(task, "Index")),
			"todayIndex":       int(asNumber(task, "TodayIndex")),
			"todayDate":        dateOnly(asString(task, "TodayIndexReferenceDate")),
			"areaIndex":        indexFor(areasByID, areaID),
			"projectIndex":     indexFor(projectsByID, projectID),
			"headingIndex":     int(asNumber(cache.State.Tasks[headingID], "Index")),
			"checklist":        checklists[id],
			"evening":          asBool(task, "Evening"),
			"repeat":           repeatSummary(task, cache.State.Tasks),
			"reminder":         reminderTime(task),
		}
		if projectID != "" {
			incrementCount(projectsByID, projectID)
		}
		if areaID != "" {
			incrementCount(areasByID, areaID)
		}
		tasks = append(tasks, modelTask)
	}

	lists := map[string][]map[string]any{}
	for _, view := range views {
		lists[view] = []map[string]any{}
	}
	for _, task := range tasks {
		for _, view := range asStringSlice(task["views"]) {
			lists[view] = append(lists[view], task)
		}
	}
	for view := range lists {
		sortTasks(lists[view], view)
	}
	sortTasks(tasks, "anytime")

	projects := valuesSorted(projectsByID)
	areas := valuesSorted(areasByID)
	tags := make([]map[string]any, 0, len(tagsByID))
	for _, tag := range tagsByID {
		tags = append(tags, tag)
	}
	sort.SliceStable(tags, func(i, j int) bool {
		if a, b := int(asNumber(tags[i], "index")), int(asNumber(tags[j], "index")); a != b {
			return a < b
		}
		return asString(tags[i], "title") < asString(tags[j], "title")
	})
	return map[string]any{
		"tags":        tags,
		"headings":    openHeadings(cache.State.Tasks, projectsByID),
		"generatedAt": time.Now().Format(time.RFC3339),
		"source":      cliPath(),
		"stats": map[string]any{
			"tasks":     len(tasks),
			"projects":  len(projects),
			"areas":     len(areas),
			"rawEvents": cache.ServerIndex,
		},
		"tasks":    tasks,
		"projects": projects,
		"areas":    areas,
		"lists":    lists,
	}, nil
}

// areaTags reads an area's tags from the cache: the patched SDK keeps the
// tag IDs (TagIDs); Tags may hold tag objects (UUID, Title) or IDs. Titles
// follow the IDs; titles of tags without a known ID come last.
func areaTags(area map[string]any, tags map[string]map[string]any) (ids, titles []string) {
	ids, titles = []string{}, []string{}
	loose := []string{}
	add := func(id string) bool {
		known, ok := tags[id]
		if ok && !slices.Contains(ids, id) {
			ids, titles = append(ids, id), append(titles, asString(known, "title"))
		}
		return ok
	}
	for _, id := range asStringSlice(area["TagIDs"]) {
		add(id)
	}
	values, _ := area["Tags"].([]any)
	for _, value := range values {
		switch tag := value.(type) {
		case string:
			add(tag)
		case map[string]any:
			if !add(asString(tag, "UUID", "uuid")) {
				if title := asString(tag, "Title", "title"); title != "" {
					loose = append(loose, title)
				}
			}
		}
	}
	return ids, append(titles, loose...)
}

func taskTagIDs(task map[string]any, tags map[string]map[string]any) []string {
	result := []string{}
	for _, id := range asStringSlice(task["TagIDs"]) {
		if _, ok := tags[id]; ok {
			result = append(result, id)
		}
	}
	return result
}

// openHeadings lists the headings of open projects, so empty headings show
// and to-dos can be dragged under them.
func openHeadings(tasks map[string]map[string]any, projects map[string]map[string]any) []map[string]any {
	result := []map[string]any{}
	for id, task := range tasks {
		if !isOpenCacheItem(task, 2) {
			continue
		}
		project := firstString(asStringSlice(task["ParentTaskIDs"]))
		title := strings.TrimSpace(asString(task, "Title"))
		if _, ok := projects[project]; !ok || title == "" {
			continue
		}
		result = append(result, map[string]any{"uuid": id, "title": title, "project": project, "index": int(asNumber(task, "Index"))})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if a, b := int(asNumber(result[i], "index")), int(asNumber(result[j], "index")); a != b {
			return a < b
		}
		return asString(result[i], "uuid") < asString(result[j], "uuid")
	})
	return result
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func titleFor(items map[string]map[string]any, id string) string {
	if id == "" {
		return ""
	}
	if item, ok := items[id]; ok {
		return fmt.Sprint(item["title"])
	}
	return ""
}

func incrementCount(items map[string]map[string]any, id string) {
	if item, ok := items[id]; ok {
		item["count"] = int(asNumber(item, "count")) + 1
	}
}

func valuesSorted(items map[string]map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	// Things also lists projects and areas without open to-dos.
	for _, item := range items {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		ic := int(asNumber(result[i], "count"))
		jc := int(asNumber(result[j], "count"))
		if ic != jc {
			return ic > jc
		}
		return fmt.Sprint(result[i]["title"]) < fmt.Sprint(result[j]["title"])
	})
	return result
}

func isOpenCacheItem(task map[string]any, itemType int) bool {
	return int(asNumber(task, "Type")) == itemType &&
		int(asNumber(task, "Status")) == 0 &&
		!asBool(task, "InTrash") &&
		asString(task, "CompletionDate", "completionDate", "completedAt") == ""
}

func taskUUIDSet(rows []map[string]any) map[string]bool {
	result := map[string]bool{}
	for _, row := range rows {
		if uuid := asString(row, "uuid", "UUID", "id"); uuid != "" {
			result[uuid] = true
		}
	}
	return result
}

func applyVisibleTaskFilter(model map[string]any, rows []map[string]any) {
	visible := taskUUIDSet(rows)
	if len(visible) == 0 {
		return
	}

	// things-cli list returns open to-dos only; completed ones not logged yet
	// come from the cache.
	keep := func(task map[string]any) bool {
		return visible[asString(task, "uuid", "UUID", "id")] || isDone(task)
	}
	tasks, _ := model["tasks"].([]map[string]any)
	nextTasks := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		if keep(task) {
			nextTasks = append(nextTasks, task)
		}
	}
	model["tasks"] = nextTasks

	lists, _ := model["lists"].(map[string][]map[string]any)
	for view, list := range lists {
		nextList := make([]map[string]any, 0, len(list))
		for _, task := range list {
			if keep(task) {
				nextList = append(nextList, task)
			}
		}
		lists[view] = nextList
	}

	recountCollections(model, nextTasks)
}

func recountCollections(model map[string]any, tasks []map[string]any) {
	projectCounts := map[string]int{}
	areaCounts := map[string]int{}
	for _, task := range tasks {
		if project := asString(task, "project"); project != "" {
			projectCounts[project]++
		}
		if area := asString(task, "area"); area != "" {
			areaCounts[area]++
		}
	}
	model["projects"] = recountCollection(model["projects"], projectCounts)
	model["areas"] = recountCollection(model["areas"], areaCounts)
}

func recountCollection(value any, counts map[string]int) []map[string]any {
	items, _ := value.([]map[string]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		// Things lists areas and projects without open tasks too.
		count := counts[asString(item, "uuid", "UUID", "id")]
		// Copy: a cached model may still be encoded by another response.
		item = maps.Clone(item)
		item["count"] = count
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		ic := int(asNumber(result[i], "count"))
		jc := int(asNumber(result[j], "count"))
		if ic != jc {
			return ic > jc
		}
		return fmt.Sprint(result[i]["title"]) < fmt.Sprint(result[j]["title"])
	})
	return result
}

func findProjectID(taskID string, tasks map[string]map[string]any) string {
	seen := map[string]bool{}
	current := taskID
	for {
		task, ok := tasks[current]
		if !ok || seen[current] {
			return ""
		}
		seen[current] = true
		parentID := firstString(asStringSlice(task["ParentTaskIDs"]))
		if parentID == "" {
			parentID = firstString(asStringSlice(task["ActionGroupIDs"]))
		}
		if parentID == "" {
			return ""
		}
		parent, ok := tasks[parentID]
		if !ok {
			return ""
		}
		if int(asNumber(parent, "Type")) == 1 {
			return parentID
		}
		current = parentID
	}
}

func findHeadingID(taskID string, tasks map[string]map[string]any) string {
	seen := map[string]bool{}
	current := taskID
	for {
		task, ok := tasks[current]
		if !ok || seen[current] {
			return ""
		}
		seen[current] = true
		parentID := firstString(asStringSlice(task["ParentTaskIDs"]))
		if parentID == "" {
			parentID = firstString(asStringSlice(task["ActionGroupIDs"]))
		}
		if parentID == "" {
			return ""
		}
		parent, ok := tasks[parentID]
		if !ok {
			return ""
		}
		if int(asNumber(parent, "Type")) == 2 {
			return parentID
		}
		if int(asNumber(parent, "Type")) == 1 {
			return ""
		}
		current = parentID
	}
}

func titleForTask(tasks map[string]map[string]any, id string) string {
	if id == "" {
		return ""
	}
	if task, ok := tasks[id]; ok {
		return asString(task, "Title", "title", "name")
	}
	return ""
}

// deadlineDate is the deadline to show. Things stores 4001-01-01 on repeating
// to-dos that have none.
func deadlineDate(value string) string {
	if date := dateOnly(value); date < "4000" {
		return date
	}
	return ""
}

func dateOnly(value string) string {
	if len(value) >= 10 {
		return value[:10]
	}
	return ""
}

// stateViews mirrors Things: Anytime also lists started tasks shown in Today,
// tasks of projects that have not started stay out of Anytime, Someday only
// lists loose tasks, and repeating templates appear in Upcoming. Today itself
// comes from the CLI.
func stateViews(task map[string]any, project map[string]any, today string) []string {
	result := []string{}
	schedule := int(asNumber(task, "Schedule"))
	scheduled := dateOnly(asString(task, "ScheduledDate", "scheduledDate", "scheduled"))
	repeating := asBool(task, "IsRepeatingTemplate")
	started := scheduled == "" || scheduled <= today
	if schedule == 0 && !repeating {
		result = append(result, "inbox")
	}
	if scheduled > today || repeating {
		result = append(result, "upcoming")
	}
	if schedule == 2 && scheduled == "" && project == nil && !repeating {
		result = append(result, "someday")
	}
	if schedule == 1 && started && projectStarted(project, today) && !repeating {
		result = append(result, "anytime")
	}
	return result
}

// projectWhen is the list Things shows a project in: "someday", "upcoming"
// (start date in the future) or "anytime". The sidebar lists only "anytime"
// projects.
func projectWhen(project map[string]any, today string) string {
	if scheduled := dateOnly(asString(project, "ScheduledDate", "scheduledDate", "scheduled")); scheduled > today {
		return "upcoming"
	}
	if int(asNumber(project, "Schedule")) == 2 {
		return "someday"
	}
	return "anytime"
}

// isDone reports a completed or canceled to-do in the model.
func isDone(task map[string]any) bool {
	status := asString(task, "status")
	return status == "completed" || status == "canceled"
}

// inTodayByDate is things-cli's Today rule: an Anytime to-do whose start
// date is today or earlier.
func inTodayByDate(task map[string]any, today string) bool {
	scheduled := dateOnly(asString(task, "ScheduledDate", "scheduledDate", "scheduled"))
	return int(asNumber(task, "Schedule")) == 1 && scheduled != "" && scheduled <= today
}

// logbookRule decides which completed to-dos Things still shows in their
// lists: with li 1 (daily, the default) those completed today after the last
// "Log Completed"; with li 2 (manually) all completed after it; with li 0
// none.
type logbookRule struct {
	logged     time.Time
	interval   int
	todayStart time.Time
}

func newLogbookRule(logDate string, interval *int, now time.Time) logbookRule {
	rule := logbookRule{interval: 1, todayStart: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())}
	if interval != nil {
		rule.interval = *interval
	}
	if parsed, err := time.Parse(time.RFC3339Nano, logDate); err == nil {
		rule.logged = parsed
	}
	return rule
}

func (r logbookRule) stillListed(task map[string]any) bool {
	return r.stillListedAs(task, 0)
}

// stillListedAs applies the rule to an item of the given type (0 to-do,
// 1 project): completed or canceled and not yet logged.
func (r logbookRule) stillListedAs(task map[string]any, itemType int) bool {
	status := int(asNumber(task, "Status"))
	if int(asNumber(task, "Type")) != itemType || (status != 2 && status != 3) || asBool(task, "InTrash") || r.interval == 0 {
		return false
	}
	completed, err := time.Parse(time.RFC3339Nano, asString(task, "CompletionDate"))
	if err != nil || !completed.After(r.logged) {
		return false
	}
	return r.interval == 2 || !completed.Before(r.todayStart)
}

func projectStarted(project map[string]any, today string) bool {
	if project == nil {
		return true
	}
	scheduled := dateOnly(asString(project, "ScheduledDate", "scheduledDate", "scheduled"))
	return int(asNumber(project, "Schedule")) == 1 && (scheduled == "" || scheduled <= today)
}

func applyCLIToday(model map[string]any, todayRows []map[string]any) {
	tasks, _ := model["tasks"].([]map[string]any)
	lists, _ := model["lists"].(map[string][]map[string]any)
	byUUID := map[string]map[string]any{}
	today := []map[string]any{}
	for _, task := range tasks {
		uuid := asString(task, "uuid", "UUID")
		if uuid == "" {
			continue
		}
		byUUID[uuid] = task
		if isDone(task) && slices.Contains(asStringSlice(task["views"]), "today") {
			// Completed to-dos keep their place in Today until logged.
			today = append(today, task)
			continue
		}
		removeView(task, "today")
	}

	for _, row := range todayRows {
		uuid := asString(row, "uuid", "UUID", "id")
		if uuid == "" {
			continue
		}
		task, ok := byUUID[uuid]
		if !ok {
			task = normalizeCLITask(row)
			tasks = append(tasks, task)
			byUUID[uuid] = task
		}
		task["views"] = appendUnique(task["views"], "today")
		task["schedule"] = "today"
		if scheduled := asString(row, "scheduledDate", "ScheduledDate"); scheduled != "" {
			task["scheduledDate"] = dateOnly(scheduled)
		}
		if deadline := asString(row, "deadlineDate", "DeadlineDate", "deadline"); deadline != "" {
			task["deadline"] = deadlineDate(deadline)
		}
		if modified := asString(row, "modificationDate", "ModificationDate", "modified"); modified != "" {
			task["modificationDate"] = dateOnly(modified)
		}
		today = append(today, task)
	}

	if lists == nil {
		lists = map[string][]map[string]any{}
		model["lists"] = lists
	}
	sortTasks(today, "today")
	lists["today"] = today
	model["tasks"] = tasks
}

func normalizeCLITask(row map[string]any) map[string]any {
	title := strings.TrimSpace(asString(row, "title", "Title", "name"))
	if title == "" {
		title = "Untitled"
	}
	return map[string]any{
		"uuid":             asString(row, "uuid", "UUID", "id"),
		"title":            title,
		"note":             cleanNote(asString(row, "note", "Note", "notes")),
		"status":           asString(row, "status", "Status"),
		"type":             "task",
		"schedule":         "today",
		"scheduledDate":    dateOnly(asString(row, "scheduledDate", "ScheduledDate")),
		"deadline":         deadlineDate(asString(row, "deadlineDate", "DeadlineDate", "deadline")),
		"modificationDate": dateOnly(asString(row, "modificationDate", "ModificationDate", "modified")),
		"project":          "",
		"projectTitle":     "",
		"heading":          "",
		"headingTitle":     "",
		"area":             "",
		"areaTitle":        "",
		"tags":             []string{},
		"views":            []string{"today"},
	}
}

func removeView(task map[string]any, view string) {
	current := asStringSlice(task["views"])
	next := make([]string, 0, len(current))
	for _, item := range current {
		if item != view {
			next = append(next, item)
		}
	}
	task["views"] = next
}

// readOnly reports whether APP_READ_ONLY forbids writes to Things Cloud, e.g.
// for test instances that use a real account.
func readOnly() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_READ_ONLY"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func validWhen(value string) bool {
	switch value {
	case "inbox", "today", "anytime", "someday":
		return true
	default:
		return false
	}
}

// sortTasks follows the order Things shows. Today, Inbox, Anytime and Someday
// group tasks by area and project (loose tasks first, then by their position).
// Today orders each group by the day a task entered Today (newest first) and
// its Today position; the other lists by heading and task position. Upcoming
// orders by start date, using the next occurrence for repeating templates.
func sortTasks(tasks []map[string]any, view string) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if view == "upcoming" {
			da, db := upcomingDate(a), upcomingDate(b)
			if da != db {
				if da == "" || db == "" {
					return db == ""
				}
				return da < db
			}
		} else {
			if c := compareTaskGroups(a, b); c != 0 {
				return c < 0
			}
			if view == "today" {
				if da, db := asString(a, "todayDate"), asString(b, "todayDate"); da != db {
					return da > db
				}
				if ta, tb := int(asNumber(a, "todayIndex")), int(asNumber(b, "todayIndex")); ta != tb {
					return ta < tb
				}
			} else if c := compareHeadings(a, b); c != 0 {
				return c < 0
			}
		}
		ii := int(asNumber(a, "index"))
		ij := int(asNumber(b, "index"))
		if ii != ij {
			return ii < ij
		}
		return strings.ToLower(fmt.Sprint(a["title"])) < strings.ToLower(fmt.Sprint(b["title"]))
	})
}

func compareTaskGroups(a, b map[string]any) int {
	if c := compareParent(a, b, "area", "areaIndex"); c != 0 {
		return c
	}
	return compareParent(a, b, "project", "projectIndex")
}

func compareHeadings(a, b map[string]any) int {
	return compareParent(a, b, "heading", "headingIndex")
}

// compareParent puts tasks without the parent first, then orders parents by
// their Things position and identifier.
func compareParent(a, b map[string]any, idKey, indexKey string) int {
	ida, idb := asString(a, idKey), asString(b, idKey)
	if ida == idb {
		return 0
	}
	if ida == "" || idb == "" {
		if ida == "" {
			return -1
		}
		return 1
	}
	if xa, xb := int(asNumber(a, indexKey)), int(asNumber(b, indexKey)); xa != xb {
		if xa < xb {
			return -1
		}
		return 1
	}
	if ida < idb {
		return -1
	}
	return 1
}

func upcomingDate(task map[string]any) string {
	if scheduled := asString(task, "scheduledDate"); scheduled != "" {
		return scheduled
	}
	return asString(task, "todayDate")
}

func indexFor(collection map[string]map[string]any, id string) int {
	if item, ok := collection[id]; ok {
		return int(asNumber(item, "index"))
	}
	return 0
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("api: panic serving %s: %v", r.URL.Path, recovered)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Internal error."})
		}
	}()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/status":
		sessionMu.RLock()
		email := session.Email
		password := session.Password
		sessionMu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"signedIn":      email != "" && password != "",
			"email":         email,
			"serverManaged": serverManagedCredentials,
			"auth":          authMode(),
			"backend":       backendInfo(),
		})
	case r.Method == http.MethodPost && r.URL.Path == "/api/login":
		if serverManagedCredentials {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "The Things account is configured on the server."})
			return
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := readJSON(r, &body); err != nil {
			errorResponse(w, err)
			return
		}
		email := strings.TrimSpace(body.Email)
		password := body.Password
		if email == "" || password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Email and password are required."})
			return
		}
		if _, err := runCLIWithCredentials(email, password, "today"); err != nil {
			errorResponse(w, err)
			return
		}
		sessionMu.Lock()
		session.Email = email
		session.Password = password
		sessionMu.Unlock()
		models.reset()
		writeJSON(w, http.StatusOK, map[string]any{"signedIn": true, "email": email, "backend": backendInfo()})
	case r.Method == http.MethodPost && r.URL.Path == "/api/logout":
		if serverManagedCredentials {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "The Things account is configured on the server."})
			return
		}
		sessionMu.Lock()
		session = sessionState{}
		worker.stop()
		sessionMu.Unlock()
		models.reset()
		writeJSON(w, http.StatusOK, map[string]any{"signedIn": false})
	case r.Method == http.MethodPost && writePaths[r.URL.Path] && readOnly():
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "This instance is read-only (APP_READ_ONLY)."})
	case r.Method == http.MethodPost && r.URL.Path == "/api/reorder":
		handleReorder(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/update":
		handleUpdate(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/checklist":
		handleChecklist(w, r)
	case r.Method == http.MethodPost && (r.URL.Path == "/api/trash" || r.URL.Path == "/api/restore"):
		handleTrash(w, r, r.URL.Path == "/api/restore")
	case r.Method == http.MethodPost && r.URL.Path == "/api/move":
		handleMove(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/project-area":
		handleProjectArea(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/schedule":
		handleSchedule(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/deadline":
		handleDeadline(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/tags":
		handleTags(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/tag-create":
		handleCreateNamed(w, r, "create-tag", "tags")
	case r.Method == http.MethodPost && r.URL.Path == "/api/area-rename":
		handleAreaRename(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/area-delete":
		handleAreaDelete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/empty-trash":
		handleEmptyTrash(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/reminder":
		handleReminder(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/repeat":
		handleRepeat(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/tag-rename":
		handleTagRename(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/tag-delete":
		handleTagDelete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/convert-project":
		handleConvertProject(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/area-create":
		handleCreateNamed(w, r, "create-area", "areas")
	case r.Method == http.MethodPost && r.URL.Path == "/api/client-error":
		handleClientError(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/reopen":
		handleReopen(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/cancel":
		handleCancel(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/project-complete":
		handleProjectComplete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/heading-delete":
		handleHeadingDelete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/log-completed":
		handleLogCompleted(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/complete":
		var body struct {
			UUID string `json:"uuid"`
		}
		if err := readJSON(r, &body); err != nil {
			errorResponse(w, err)
			return
		}
		body.UUID = strings.TrimSpace(body.UUID)
		if !validThingsID(body.UUID) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A valid task UUID is required."})
			return
		}
		if _, err := withWriteRetry(func() ([]byte, error) { return runCLIOutput("complete", body.UUID) }); err != nil {
			errorResponse(w, err)
			return
		}
		// Like Things, the to-do stays in its list, ticked, until it is logged.
		models.wrote(withTask(body.UUID, func(task map[string]any) { task["status"] = "completed" }))
		writeJSON(w, http.StatusOK, map[string]any{"completed": true, "uuid": body.UUID})
	case r.Method == http.MethodPost && r.URL.Path == "/api/create":
		var body struct {
			Type      string `json:"type"`
			Title     string `json:"title"`
			Note      string `json:"note"`
			When      string `json:"when"`
			Scheduled string `json:"scheduled"`
			Project   string `json:"project"`
			Heading   string `json:"heading"`
			Area      string `json:"area"`
			// Placed: the page moves the new to-do itself (below a selected
			// one), so it is not moved to the top.
			Placed bool `json:"placed"`
		}
		if err := readJSON(r, &body); err != nil {
			errorResponse(w, err)
			return
		}
		title := strings.TrimSpace(body.Title)
		note := strings.TrimSpace(body.Note)
		if title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Task title is required."})
			return
		}
		if len(title) > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Task title is too long."})
			return
		}
		if len(note) > 5000 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Task note is too long."})
			return
		}
		for _, value := range []string{title, note} {
			if !safeCLIValue(value) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Title and note must not start with --."})
				return
			}
		}
		for _, id := range []string{body.Project, body.Heading, body.Area} {
			if id = strings.TrimSpace(id); id != "" && !validThingsID(id) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project, heading or area."})
				return
			}
		}
		kind := strings.TrimSpace(body.Type)
		if kind == "" {
			kind = "task"
		}
		if kind != "task" && kind != "project" && kind != "heading" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unsupported type."})
			return
		}
		if kind == "heading" && (!validThingsID(strings.TrimSpace(body.Project)) || strings.TrimSpace(body.Heading) != "" || strings.TrimSpace(body.Area) != "") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A heading belongs to a project."})
			return
		}
		// A start date puts the task in Upcoming (or Today/Anytime when it
		// has passed); it replaces the list.
		scheduled := strings.TrimSpace(body.Scheduled)
		if scheduled != "" {
			if _, err := time.Parse("2006-01-02", scheduled); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The start date must be YYYY-MM-DD."})
				return
			}
		}
		when := strings.TrimSpace(body.When)
		if when == "" && scheduled == "" {
			when = "inbox"
			if kind != "task" {
				when = "anytime"
			}
		}
		if when != "" && !validWhen(when) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unsupported task list."})
			return
		}
		if kind == "project" && (when == "inbox" || strings.TrimSpace(body.Project) != "" || strings.TrimSpace(body.Heading) != "") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A project can only belong to an area."})
			return
		}
		op := map[string]any{"cmd": "create", "title": title}
		if kind != "task" {
			op["type"] = kind
		}
		if when != "" {
			op["when"] = when
		}
		if scheduled != "" {
			op["extra"] = map[string]string{"scheduled": scheduled}
		}
		if note != "" {
			op["note"] = note
		}
		for key, value := range map[string]string{"project": body.Project, "heading": body.Heading, "area": body.Area} {
			if value = strings.TrimSpace(value); value != "" {
				op[key] = value
			}
		}
		input, _ := json.Marshal([]map[string]any{op})
		out, err := runCLIBatch(input)
		if err != nil {
			errorResponse(w, err)
			return
		}
		var created struct {
			Results []struct {
				UUID string `json:"uuid"`
			} `json:"results"`
			UUID string `json:"uuid"`
		}
		_ = json.Unmarshal(out, &created)
		if len(created.Results) > 0 {
			created.UUID = created.Results[0].UUID
		}
		var task map[string]any
		if kind == "task" && created.UUID != "" {
			task = newModelTask(created.UUID, title, note, when, scheduled, strings.TrimSpace(body.Project), strings.TrimSpace(body.Area))
			// Task7 creates are verified at position 0 only; a second write
			// moves the new to-do to the top of its list. It runs after the
			// answer, so the new to-do is ready at once.
			var moveInput []byte
			if index, todayIndex, ok := topPositions(when, strings.TrimSpace(body.Project), strings.TrimSpace(body.Area)); ok && !body.Placed {
				move := map[string]any{"cmd": "reorder", "uuid": created.UUID, "index": index}
				task["index"] = index
				if when == "today" {
					move["todayIndex"] = todayIndex
					task["todayIndex"] = todayIndex
				}
				moveInput, _ = json.Marshal([]map[string]any{move})
			}
			models.wrote(withNewTask(task))
			if moveInput != nil {
				go moveInBackground(moveInput, "create: moving the new to-do to the top failed", created.UUID)
			}
		} else if kind == "heading" && created.UUID != "" {
			project := strings.TrimSpace(body.Project)
			heading := map[string]any{"uuid": created.UUID, "title": title, "project": project, "index": 0}
			// Like Things, a new heading goes to the end of the project.
			index, ok := lastHeadingIndex(project)
			if ok {
				heading["index"] = index
			}
			models.wrote(withNewHeading(heading))
			if ok {
				move, _ := json.Marshal([]map[string]any{{"cmd": "reorder", "uuid": created.UUID, "index": index}})
				go moveInBackground(move, "create: moving the new heading to the end failed", created.UUID)
			}
		} else {
			models.wrote(nil)
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": true, "when": when, "type": kind, "uuid": created.UUID, "task": task})
	case r.Method == http.MethodGet && (r.URL.Path == "/api/logbook" || r.URL.Path == "/api/trash"):
		handleArchive(w, r, r.URL.Path == "/api/trash")
	case r.Method == http.MethodGet && r.URL.Path == "/api/search":
		handleSearch(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/sync":
		// Answer from the cached model; ?fresh=1 waits for a new sync.
		get := models.cached
		if r.URL.Query().Get("fresh") == "1" {
			get = models.fresh
		}
		result, err := get()
		if err != nil {
			errorResponse(w, err)
			return
		}
		response := maps.Clone(result.model)
		response["syncedAt"] = result.syncedAt.Format(time.RFC3339)
		response["refreshing"] = result.refreshing
		if result.syncErr != nil {
			response["syncError"] = result.syncErr.Error()
		}
		writeJSON(w, http.StatusOK, response)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not found."})
	}
}

func staticHandler(w http.ResponseWriter, r *http.Request) {
	root := filepath.Join(appRoot(), "public")
	requestPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if requestPath == "." || requestPath == "" {
		requestPath = "index.html"
	}
	fullPath := filepath.Join(root, requestPath)
	rel, relErr := filepath.Rel(root, fullPath)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		data, err = os.ReadFile(filepath.Join(root, "index.html"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		fullPath = filepath.Join(root, "index.html")
	}
	if contentType := mime.TypeByExtension(filepath.Ext(fullPath)); contentType != "" {
		w.Header().Set("content-type", contentType)
	}
	_, _ = w.Write(data)
}

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

func main() {
	setupLogging()
	if len(os.Args) > 1 {
		if strings.HasPrefix(os.Args[1], "mcp-token-") {
			os.Exit(runMCPTokenCommand(os.Args[1:], os.Stdout, os.Stderr))
		}
		os.Exit(runPasskeyCommand(os.Args[1:], os.Stdout, os.Stderr))
	}
	port := firstEnv("PORT")
	if port == "" {
		port = "4173"
	}
	host := firstEnv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	if err := validateSecurityConfig(host); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", apiHandler)
	mux.HandleFunc("/", staticHandler)
	address := host + ":" + port
	log.Printf("NubeRerum is running at http://%s", address)
	if serverManagedCredentials && cliPath() != "" {
		// Warm the model so the first page load does not wait for a sync.
		models.refresh()
	}
	server := newHTTPServer(address, appHandler(mux))
	log.Fatal(server.ListenAndServe())
}
