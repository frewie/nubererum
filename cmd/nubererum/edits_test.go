package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	editTask  = "TaskAaaaaaaaaaaaaaaaaaa"
	editItem1 = "ItemAaaaaaaaaaaaaaaaaaa"
	editItem2 = "ItemBbbbbbbbbbbbbbbbbbb"
)

// editHarness fakes things-cli: it records each call and answers batch with
// a result per operation.
func editHarness(t *testing.T) (post func(path, body string) *httptest.ResponseRecorder, calls func() string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	cli := filepath.Join(dir, "things-cli")
	script := `#!/bin/sh
if [ "$1" = batch ]; then
  input=$(cat)
  printf 'batch %s\n' "$input" >> "` + log + `"
  echo '{"status":"ok","results":[{"cmd":"x","uuid":"ItemNewwwwwwwwwwwwwwwww"}]}'
else
  printf '%s|' "$@" >> "` + log + `"; echo >> "` + log + `"
  echo '{"status":"updated"}'
fi
`
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("APP_READ_ONLY", "")
	previousModels := models
	built := false
	models = newModelCache(func() (map[string]any, error) {
		// Only the first sync answers; later ones (started by writes) never
		// finish, so the test sees the patched cached model.
		if built {
			select {}
		}
		built = true
		task := map[string]any{
			"uuid": editTask, "title": "Old", "note": "", "views": []string{"today"},
			"checklist": []map[string]any{
				{"uuid": editItem1, "title": "One", "done": false, "index": 0},
				{"uuid": editItem2, "title": "Two", "done": false, "index": 1},
			},
		}
		return map[string]any{
			"tasks":    []map[string]any{task},
			"lists":    map[string][]map[string]any{"today": {task}},
			"projects": []map[string]any{},
			"areas":    []map[string]any{},
		}, nil
	})
	sessionMu.Lock()
	previousSession := session
	session = sessionState{Email: "user@example.com", Password: "secret"}
	sessionMu.Unlock()
	t.Cleanup(func() {
		models = previousModels
		sessionMu.Lock()
		session = previousSession
		sessionMu.Unlock()
	})
	post = func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("content-type", "application/json")
		response := httptest.NewRecorder()
		apiHandler(response, request)
		return response
	}
	calls = func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
	return post, calls
}

func cachedTask(t *testing.T) map[string]any {
	t.Helper()
	models.mu.Lock()
	defer models.mu.Unlock()
	for _, task := range models.model["tasks"].([]map[string]any) {
		if task["uuid"] == editTask {
			return task
		}
	}
	return nil
}

func TestUpdateEditsTitleAndNotes(t *testing.T) {
	post, calls := editHarness(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	if response := post("/api/update", `{"uuid":"`+editTask+`","title":" New title ","note":"Line 1\nLine 2\n"}`); response.Code != http.StatusOK {
		t.Fatalf("update = %d %s", response.Code, response.Body)
	}
	if got := calls(); got != "edit|"+editTask+"|--title|New title|--note|Line 1\nLine 2|\n" {
		t.Fatalf("CLI call = %q", got)
	}
	if task := cachedTask(t); task["title"] != "New title" || task["note"] != "Line 1\nLine 2" {
		t.Fatalf("cached task = %v", task)
	}
	// Clearing the notes passes an empty value.
	if response := post("/api/update", `{"uuid":"`+editTask+`","note":""}`); response.Code != http.StatusOK {
		t.Fatalf("clear note = %d", response.Code)
	}
	for _, bad := range []string{`{"uuid":"` + editTask + `"}`, `{"uuid":"` + editTask + `","title":"  "}`, `{"uuid":"` + editTask + `","title":"--help"}`} {
		if response := post("/api/update", bad); response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", bad, response.Code)
		}
	}
}

func TestChecklistActions(t *testing.T) {
	post, calls := editHarness(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	steps := []struct{ body, want string }{
		{`{"action":"add","task":"` + editTask + `","title":"Three"}`, `{"cmd":"checklist-add","index":513,"task":"` + editTask + `","title":"Three"}`},
		{`{"action":"edit","task":"` + editTask + `","uuid":"` + editItem1 + `","title":"Uno"}`, `{"cmd":"checklist-edit","title":"Uno","uuid":"` + editItem1 + `"}`},
		{`{"action":"complete","task":"` + editTask + `","uuid":"` + editItem2 + `","done":true}`, `{"cmd":"checklist-complete","done":true,"uuid":"` + editItem2 + `"}`},
		{`{"action":"reorder","task":"` + editTask + `","uuid":"` + editItem2 + `","order":["` + editItem2 + `","` + editItem1 + `","ItemNewwwwwwwwwwwwwwwww"]}`, `{"cmd":"checklist-reorder","index":-512,"uuid":"` + editItem2 + `"}`},
		{`{"action":"delete","task":"` + editTask + `","uuid":"` + editItem1 + `"}`, `{"cmd":"checklist-delete","uuid":"` + editItem1 + `"}`},
	}
	for _, step := range steps {
		if response := post("/api/checklist", step.body); response.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", step.body, response.Code, response.Body)
		}
		if !strings.Contains(calls(), "batch ["+step.want+"]") {
			t.Fatalf("after %s calls =\n%s\nwant %s", step.body, calls(), step.want)
		}
	}
	items := cachedTask(t)["checklist"].([]map[string]any)
	if len(items) != 2 || items[0]["uuid"] != editItem2 || items[0]["done"] != true || items[1]["uuid"] != "ItemNewwwwwwwwwwwwwwwww" || items[1]["title"] != "Three" {
		t.Fatalf("cached checklist = %v", items)
	}
	if response := post("/api/checklist", `{"action":"edit","task":"`+editTask+`","uuid":"ItemMissssssssssssssss1","title":"x"}`); response.Code != http.StatusConflict {
		t.Fatalf("unknown item = %d, want 409", response.Code)
	}
}

func TestTrashAndRestore(t *testing.T) {
	post, calls := editHarness(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	if response := post("/api/trash", `{"uuids":["`+editTask+`"]}`); response.Code != http.StatusOK {
		t.Fatalf("trash = %d %s", response.Code, response.Body)
	}
	if cachedTask(t) != nil {
		t.Fatal("trashed to-do still in the cached model")
	}
	if response := post("/api/restore", `{"uuids":["`+editTask+`"]}`); response.Code != http.StatusOK {
		t.Fatalf("restore = %d", response.Code)
	}
	if got := calls(); !strings.Contains(got, `batch [{"cmd":"trash","uuid":"`+editTask+`"}]`) || !strings.Contains(got, `batch [{"cmd":"restore","uuid":"`+editTask+`"}]`) {
		t.Fatalf("calls = %s", got)
	}
	t.Setenv("APP_READ_ONLY", "1")
	for _, path := range []string{"/api/trash", "/api/update", "/api/checklist", "/api/restore"} {
		if response := post(path, `{}`); response.Code != http.StatusForbidden {
			t.Fatalf("read-only %s = %d, want 403", path, response.Code)
		}
	}
}

func TestChecklistsByTaskOrdersItems(t *testing.T) {
	got := checklistsByTask(map[string]map[string]any{
		"b": {"Title": "Second", "Index": 5, "Status": 3, "TaskIDs": []any{"t"}},
		"a": {"Title": "First", "Index": -2, "Status": 0, "TaskIDs": []any{"t"}},
		"x": {"Title": "", "Index": 0, "TaskIDs": []any{"t"}},
	})["t"]
	if len(got) != 2 || got[0]["title"] != "First" || got[1]["done"] != true {
		t.Fatalf("checklist = %v", got)
	}
}

func TestReopenAndLogCompleted(t *testing.T) {
	post, calls := editHarness(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	models.wrote(withTask(editTask, func(task map[string]any) { task["status"] = "completed" }))
	if response := post("/api/reopen", `{"uuid":"`+editTask+`"}`); response.Code != http.StatusOK {
		t.Fatalf("reopen = %d %s", response.Code, response.Body)
	}
	if cachedTask(t)["status"] != "open" {
		t.Fatal("reopened to-do is not open in the cached model")
	}
	models.wrote(withTask(editTask, func(task map[string]any) { task["status"] = "completed" }))
	if response := post("/api/log-completed", `{}`); response.Code != http.StatusOK {
		t.Fatalf("log = %d %s", response.Code, response.Body)
	}
	if cachedTask(t) != nil {
		t.Fatal("logged to-do still listed")
	}
	if got := calls(); !strings.Contains(got, `batch [{"cmd":"reopen","uuid":"`+editTask+`"}]`) || !strings.Contains(got, `batch [{"cmd":"log-completed"}]`) {
		t.Fatalf("calls = %s", got)
	}
}

func TestRedactHidesTheAccount(t *testing.T) {
	sessionMu.Lock()
	previous := session
	session = sessionState{Email: "owner@example.com", Password: "hunter2-secret"}
	sessionMu.Unlock()
	t.Cleanup(func() {
		sessionMu.Lock()
		session = previous
		sessionMu.Unlock()
	})
	got := redact(`Get "https://cloud.culturedcode.com/version/1/account/owner@example.com": password hunter2-secret, owner%40example.com`)
	if strings.Contains(got, "owner") || strings.Contains(got, "hunter2") {
		t.Fatalf("redact = %q", got)
	}
}

func TestLogErrorsLogsFailedRequests(t *testing.T) {
	var buffer strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := logErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/create":
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Things Cloud write failed: create ix must be 0"})
		case "/api/sync":
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Sign in."})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	for _, path := range []string{"/api/create", "/api/sync", "/api/status"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}
	logged := buffer.String()
	if !strings.Contains(logged, "level=ERROR") || !strings.Contains(logged, "path=/api/create") || !strings.Contains(logged, "create ix must be 0") {
		t.Fatalf("log = %s", logged)
	}
	if strings.Contains(logged, "/api/sync") || strings.Contains(logged, "/api/status") {
		t.Fatalf("expected 401 and success were logged: %s", logged)
	}
}

func TestScheduleDeadlineAndTags(t *testing.T) {
	post, calls := editHarness(t)
	models = newModelCache(func() (map[string]any, error) {
		task := map[string]any{"uuid": editTask, "title": "Old", "views": []string{"today", "anytime"}, "tags": []string{}, "tagIDs": []string{}}
		return map[string]any{
			"tasks": []map[string]any{task},
			"lists": map[string][]map[string]any{"today": {task}, "anytime": {task}},
			"tags":  []map[string]any{{"uuid": "TagErrandddddddddddddd1", "title": "Errand"}},
		}, nil
	})
	steps := []struct{ path, body, want string }{
		{"/api/schedule", `{"uuids":["` + editTask + `"],"when":"evening"}`, `{"cmd":"schedule","uuid":"` + editTask + `","when":"evening"}`},
		{"/api/schedule", `{"uuids":["` + editTask + `"],"when":"someday"}`, `{"cmd":"schedule","uuid":"` + editTask + `","when":"someday"}`},
		{"/api/schedule", `{"uuids":["` + editTask + `"],"startDate":"2099-03-04"}`, `{"cmd":"schedule","startDate":"2099-03-04","uuid":"` + editTask + `"}`},
		{"/api/deadline", `{"uuid":"` + editTask + `","deadline":"2099-05-06"}`, `{"cmd":"deadline","deadlineDate":"2099-05-06","uuid":"` + editTask + `"}`},
		{"/api/deadline", `{"uuid":"` + editTask + `","deadline":""}`, `{"cmd":"deadline","deadlineDate":"","uuid":"` + editTask + `"}`},
		{"/api/tags", `{"uuid":"` + editTask + `","tags":["TagErrandddddddddddddd1"]}`, `{"cmd":"tags","tags":["TagErrandddddddddddddd1"],"uuid":"` + editTask + `"}`},
	}
	for _, step := range steps {
		if response := post(step.path, step.body); response.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", step.path, step.body, response.Code, response.Body)
		}
		if !strings.Contains(calls(), step.want) {
			t.Fatalf("calls =\n%s\nwant %s", calls(), step.want)
		}
	}
	for path, body := range map[string]string{
		"/api/schedule": `{"uuids":["` + editTask + `"],"when":"tomorrow"}`,
		"/api/deadline": `{"uuid":"` + editTask + `","deadline":"06.05.2099"}`,
	} {
		if response := post(path, body); response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s = %d, want 400", path, body, response.Code)
		}
	}
	if response := post("/api/tags", `{"uuid":"`+editTask+`","tags":["TagUnknownnnnnnnnnnnnn1"]}`); response.Code != http.StatusConflict {
		t.Fatalf("unknown tag = %d, want 409", response.Code)
	}
}

func TestWithTasksChangedMovesBetweenLists(t *testing.T) {
	task := map[string]any{"uuid": "a", "views": []string{"today", "anytime"}}
	model := map[string]any{"tasks": []map[string]any{task}, "lists": map[string][]map[string]any{"today": {task}, "anytime": {task}, "upcoming": {}}}
	next := withTasksChanged([]string{"a"}, func(task map[string]any) { task["views"] = []string{"upcoming"} })(model)
	lists := next["lists"].(map[string][]map[string]any)
	if len(lists["today"]) != 0 || len(lists["anytime"]) != 0 || len(lists["upcoming"]) != 1 {
		t.Fatalf("lists = %v", lists)
	}
	if len(model["lists"].(map[string][]map[string]any)["today"]) != 1 {
		t.Fatal("original model changed")
	}
}

func TestLogbookAndTrashFromCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache.json")
	t.Setenv("THINGS_CLI_CACHE", cache)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)
	older := now.Add(-72 * time.Hour).Format(time.RFC3339)
	data := `{"version":6,"historyId":"h","serverIndex":1,"state":{
	  "Areas":{"a":{"UUID":"a","Title":"Home"}},
	  "Tasks":{
	    "p":{"UUID":"p","Title":"Garden","Type":1,"Status":0,"AreaIDs":["a"]},
	    "done1":{"UUID":"done1","Title":"Mow","Type":0,"Status":3,"CompletionDate":"` + old + `","ParentTaskIDs":["p"]},
	    "done2":{"UUID":"done2","Title":"Rake","Type":0,"Status":2,"CompletionDate":"` + older + `"},
	    "today":{"UUID":"today","Title":"Still listed","Type":0,"Status":3,"CompletionDate":"` + now.Format(time.RFC3339) + `"},
	    "bin":{"UUID":"bin","Title":"Old idea","Type":0,"Status":0,"InTrash":true,"ModificationDate":"` + old + `"},
	    "open":{"UUID":"open","Title":"Open","Type":0,"Status":0}
	  },
	  "Tags":{},"CheckListItems":{}}}`
	if err := os.WriteFile(cache, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	get := func(path string) []map[string]any {
		response := httptest.NewRecorder()
		apiHandler(response, httptest.NewRequest(http.MethodGet, path, nil))
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v %s", path, err, response.Body)
		}
		return body.Items
	}
	logbook := get("/api/logbook")
	if len(logbook) != 2 || logbook[0]["uuid"] != "done1" || logbook[0]["projectTitle"] != "Garden" || logbook[0]["areaTitle"] != "Home" || logbook[1]["status"] != "canceled" {
		t.Fatalf("logbook = %v", logbook)
	}
	trash := get("/api/trash")
	if len(trash) != 1 || trash[0]["uuid"] != "bin" {
		t.Fatalf("trash = %v", trash)
	}
}

func TestTrashProjectTakesItsToDosAlong(t *testing.T) {
	post, calls := editHarness(t)
	const (
		project = "ProjectTrashhhhhhhhhhh1"
		heading = "HeadingTrashhhhhhhhhhh1"
		direct  = "ToDoDirectttttttttttttt"
		under   = "ToDoUnderHeadinggggggg1"
		binned  = "ToDoBinnedddddddddddddd"
		other   = "ToDoOtherrrrrrrrrrrrrrr"
	)
	cache := filepath.Join(t.TempDir(), "cache.json")
	t.Setenv("THINGS_CLI_CACHE", cache)
	data := `{"version":6,"historyId":"h","serverIndex":1,"state":{"Areas":{},"Tags":{},"CheckListItems":{},"Tasks":{
	  "` + project + `":{"Title":"Relocation","Type":1,"Status":0},
	  "` + heading + `":{"Title":"Packing","Type":2,"ParentTaskIDs":["` + project + `"]},
	  "` + direct + `":{"Title":"Boxes","Type":0,"Status":3,"ParentTaskIDs":["` + project + `"]},
	  "` + under + `":{"Title":"Tape","Type":0,"Status":0,"ActionGroupIDs":["` + heading + `"]},
	  "` + binned + `":{"Title":"Old","Type":0,"Status":0,"InTrash":true,"ParentTaskIDs":["` + project + `"]},
	  "` + other + `":{"Title":"Elsewhere","Type":0,"Status":0}}}}`
	if err := os.WriteFile(cache, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if response := post("/api/trash", `{"uuids":["`+project+`"]}`); response.Code != http.StatusOK {
		t.Fatalf("trash = %d %s", response.Code, response.Body)
	}
	want := `batch [{"cmd":"trash","uuid":"` + project + `"},{"cmd":"trash","uuid":"` + direct + `"},{"cmd":"trash","uuid":"` + under + `"}]`
	if got := calls(); !strings.Contains(got, want) {
		t.Fatalf("calls = %s\nwant %s", got, want)
	}
	if response := post("/api/restore", `{"uuids":["`+project+`"]}`); response.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", response.Code, response.Body)
	}
	want = `batch [{"cmd":"restore","uuid":"` + project + `"},{"cmd":"restore","uuid":"` + binned + `"}]`
	if got := calls(); !strings.Contains(got, want) {
		t.Fatalf("calls = %s\nwant %s", got, want)
	}
}

func TestWithoutProjectDropsItsRows(t *testing.T) {
	inside := map[string]any{"uuid": "a", "project": "p"}
	outside := map[string]any{"uuid": "b", "project": "q"}
	model := map[string]any{
		"tasks":    []map[string]any{inside, outside},
		"lists":    map[string][]map[string]any{"anytime": {inside, outside}},
		"projects": []map[string]any{{"uuid": "p", "title": "P"}, {"uuid": "q", "title": "Q"}},
		"headings": []map[string]any{{"uuid": "h", "project": "p"}},
		"areas":    []map[string]any{},
	}
	next := withoutProject("p")(model)
	if tasks := next["tasks"].([]map[string]any); len(tasks) != 1 || tasks[0]["uuid"] != "b" {
		t.Fatalf("tasks = %v", tasks)
	}
	if projects := next["projects"].([]map[string]any); len(projects) != 1 || projects[0]["uuid"] != "q" {
		t.Fatalf("projects = %v", projects)
	}
	if len(next["headings"].([]map[string]any)) != 0 || len(next["lists"].(map[string][]map[string]any)["anytime"]) != 1 {
		t.Fatalf("model = %v", next)
	}
	if len(model["tasks"].([]map[string]any)) != 2 {
		t.Fatal("original model changed")
	}
}

func TestRenameProjectAndArea(t *testing.T) {
	post, calls := editHarness(t)
	const project, area = "ProjectRenameeeeeeeeee1", "AreaRenameeeeeeeeeeeee"
	task := map[string]any{"uuid": editTask, "title": "Pack", "project": project, "projectTitle": "Old", "area": area, "areaTitle": "Home", "views": []string{"anytime"}}
	built := false
	models = newModelCache(func() (map[string]any, error) {
		// Syncs after the writes never finish: the test sees the patches.
		if built {
			select {}
		}
		built = true
		return map[string]any{
			"tasks":    []map[string]any{task},
			"lists":    map[string][]map[string]any{"anytime": {task}},
			"projects": []map[string]any{{"uuid": project, "title": "Old", "note": ""}},
			"areas":    []map[string]any{{"uuid": area, "title": "Home"}},
		}, nil
	})
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	if response := post("/api/update", `{"uuid":"`+project+`","title":" Relocation ","note":"Boxes first"}`); response.Code != http.StatusOK {
		t.Fatalf("update = %d %s", response.Code, response.Body)
	}
	if response := post("/api/area-rename", `{"uuid":"`+area+`","title":"Family"}`); response.Code != http.StatusOK {
		t.Fatalf("area rename = %d %s", response.Code, response.Body)
	}
	if response := post("/api/area-rename", `{"uuid":"`+area+`","title":"  "}`); response.Code != http.StatusBadRequest {
		t.Fatalf("empty area name = %d, want 400", response.Code)
	}
	got := calls()
	if !strings.Contains(got, "edit|"+project+"|--title|Relocation|--note|Boxes first|") || !strings.Contains(got, `batch [{"cmd":"area-rename","title":"Family","uuid":"`+area+`"}]`) {
		t.Fatalf("calls = %s", got)
	}
	models.mu.Lock()
	model := models.model
	models.mu.Unlock()
	if p := model["projects"].([]map[string]any)[0]; p["title"] != "Relocation" || p["note"] != "Boxes first" {
		t.Fatalf("project = %v", p)
	}
	if a := model["areas"].([]map[string]any)[0]; a["title"] != "Family" {
		t.Fatalf("area = %v", a)
	}
	listed := model["lists"].(map[string][]map[string]any)["anytime"][0]
	if row := model["tasks"].([]map[string]any)[0]; row["projectTitle"] != "Relocation" || row["areaTitle"] != "Family" || listed["projectTitle"] != "Relocation" {
		t.Fatalf("task = %v, listed = %v", row, listed)
	}
	if task["projectTitle"] != "Old" {
		t.Fatal("original row changed")
	}
	t.Setenv("APP_READ_ONLY", "1")
	if response := post("/api/area-rename", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read-only area-rename = %d, want 403", response.Code)
	}
	if response := post("/api/area-delete", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read-only area-delete = %d, want 403", response.Code)
	}
}

func TestScheduleEveningPatchesTheModel(t *testing.T) {
	post, _ := editHarness(t)
	built := false
	models = newModelCache(func() (map[string]any, error) {
		// Later syncs (started by the writes) never finish, so the test sees
		// the patched model.
		if built {
			select {}
		}
		built = true
		task := map[string]any{"uuid": editTask, "title": "Old", "views": []string{"anytime"}, "evening": false}
		return map[string]any{"tasks": []map[string]any{task}, "lists": map[string][]map[string]any{"anytime": {task}, "today": {}}}, nil
	})
	evening := func() map[string]any {
		t.Helper()
		result, err := models.cached()
		if err != nil {
			t.Fatal(err)
		}
		return result.model["tasks"].([]map[string]any)[0]
	}
	if response := post("/api/schedule", `{"uuids":["`+editTask+`"],"when":"evening"}`); response.Code != http.StatusOK {
		t.Fatalf("evening = %d %s", response.Code, response.Body)
	}
	if task := evening(); task["evening"] != true || !slices.Contains(asStringSlice(task["views"]), "today") || task["scheduledDate"] != time.Now().Format("2006-01-02") {
		t.Fatalf("after evening: %v", task)
	}
	if response := post("/api/schedule", `{"uuids":["`+editTask+`"],"when":"today"}`); response.Code != http.StatusOK {
		t.Fatalf("today = %d %s", response.Code, response.Body)
	}
	if task := evening(); task["evening"] != false || !slices.Contains(asStringSlice(task["views"]), "today") {
		t.Fatalf("after today: %v", task)
	}
}

func TestCancelWritesOneOpPerToDoAndCrossesThemOut(t *testing.T) {
	post, calls := editHarness(t)
	built := false
	models = newModelCache(func() (map[string]any, error) {
		// Later syncs (started by the write) never finish, so the test sees
		// the patched model.
		if built {
			select {}
		}
		built = true
		task := map[string]any{"uuid": editTask, "title": "Old", "status": "open", "views": []string{"today"}}
		return map[string]any{"tasks": []map[string]any{task}, "lists": map[string][]map[string]any{"today": {task}}}, nil
	})
	if response := post("/api/cancel", `{"uuids":["`+editTask+`"]}`); response.Code != http.StatusOK {
		t.Fatalf("cancel = %d %s", response.Code, response.Body)
	}
	if want := `[{"cmd":"cancel","uuid":"` + editTask + `"}]`; !strings.Contains(calls(), want) {
		t.Fatalf("calls =\n%s\nwant %s", calls(), want)
	}
	result, _ := models.cached()
	if task := result.model["tasks"].([]map[string]any)[0]; task["status"] != "canceled" || !slices.Contains(asStringSlice(task["views"]), "today") {
		t.Fatalf("after cancel: %v", task)
	}
	for body, want := range map[string]int{
		`{"uuids":[]}`:                         http.StatusBadRequest,
		`{"uuids":["--flag"]}`:                 http.StatusBadRequest,
		`{"uuids":["UnknownTaskkkkkkkkkkkk"]}`: http.StatusConflict,
	} {
		if response := post("/api/cancel", body); response.Code != want {
			t.Fatalf("cancel %s = %d, want %d", body, response.Code, want)
		}
	}
}

func TestReminderNeedsADateAndWritesItsStartDate(t *testing.T) {
	if got := reminderTime(map[string]any{"AlarmTimeOffset": float64(45000)}); got != "12:30" {
		t.Fatalf("reminderTime = %q", got)
	}
	if got := reminderTime(map[string]any{}); got != "" {
		t.Fatalf("reminderTime without ato = %q", got)
	}
	post, calls := editHarness(t)
	if response := post("/api/reminder", `{"uuid":"`+editTask+`","time":"12:30"}`); response.Code != http.StatusOK {
		t.Fatalf("set = %d %s", response.Code, response.Body)
	}
	today := time.Now().Format("2006-01-02")
	if got := calls(); !strings.Contains(got, `batch [{"cmd":"reminder","startDate":"`+today+`","time":"12:30","uuid":"`+editTask+`"}]`) {
		t.Fatalf("calls = %s", got)
	}
	result, _ := models.cached()
	if task := result.model["tasks"].([]map[string]any)[0]; task["reminder"] != "12:30" {
		t.Fatalf("patched task = %v", task)
	}
	if response := post("/api/reminder", `{"uuid":"`+editTask+`","time":""}`); response.Code != http.StatusOK {
		t.Fatalf("remove = %d %s", response.Code, response.Body)
	}
	if got := calls(); !strings.Contains(got, `batch [{"cmd":"reminder","time":"","uuid":"`+editTask+`"}]`) {
		t.Fatalf("calls = %s", got)
	}
	for _, body := range []string{`{"uuid":"` + editTask + `","time":"25:00"}`, `{"uuid":"` + editTask + `","time":"9:00"}`, `{"uuid":"--x","time":"09:00"}`} {
		if response := post("/api/reminder", body); response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", body, response.Code)
		}
	}

	// A to-do without a date (Anytime) cannot take a reminder.
	writeProjectCache(t)
	projectModel(t)
	if response := post("/api/reminder", `{"uuid":"`+projToDo1+`","time":"09:00"}`); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "needs a date") {
		t.Fatalf("no date = %d %s", response.Code, response.Body)
	}
	t.Setenv("APP_READ_ONLY", "1")
	if response := post("/api/reminder", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read-only reminder = %d, want 403", response.Code)
	}
}

func TestSearchFindsLoggedItemsByTitleAndNotes(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache.json")
	t.Setenv("THINGS_CLI_CACHE", cache)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)
	older := now.Add(-72 * time.Hour).Format(time.RFC3339)
	data := `{"version":8,"historyId":"h","serverIndex":1,"state":{"Areas":{},"Tags":{},"CheckListItems":{},
	  "Tasks":{
	    "mow1":{"UUID":"mow1","Title":"Mow the lawn","Type":0,"Status":3,"CompletionDate":"` + older + `"},
	    "mow2":{"UUID":"mow2","Title":"Fix mower","Type":1,"Status":2,"CompletionDate":"` + old + `"},
	    "rake":{"UUID":"rake","Title":"Rake","Note":"before the MOWING","Type":0,"Status":3,"CompletionDate":"` + older + `"},
	    "listed":{"UUID":"listed","Title":"Mow again","Type":0,"Status":3,"CompletionDate":"` + now.Format(time.RFC3339) + `"},
	    "bin":{"UUID":"bin","Title":"Mow in the Trash","Type":0,"Status":3,"InTrash":true,"CompletionDate":"` + old + `"},
	    "open":{"UUID":"open","Title":"Mow open","Type":0,"Status":0}
	  }}}`
	if err := os.WriteFile(cache, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	search := func(q string) (int, []string) {
		response := httptest.NewRecorder()
		apiHandler(response, httptest.NewRequest(http.MethodGet, "/api/search?q="+url.QueryEscape(q), nil))
		var body struct {
			Items []map[string]any `json:"items"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		ids := []string{}
		for _, item := range body.Items {
			ids = append(ids, asString(item, "uuid"))
		}
		return response.Code, ids
	}
	// Newest first; notes count; not open, still listed or trashed items.
	if code, ids := search(" MOW "); code != http.StatusOK || strings.Join(ids, ",") != "mow2,mow1,rake" {
		t.Fatalf("search = %d %v", code, ids)
	}
	for _, q := range []string{"m", strings.Repeat("x", 201)} {
		if code, _ := search(q); code != http.StatusBadRequest {
			t.Fatalf("search %q = %d, want 400", q[:1], code)
		}
	}
}
