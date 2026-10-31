package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStateViewsMatchScheduleBuckets(t *testing.T) {
	today := "2026-06-04"
	activeProject := map[string]any{"Type": float64(1), "Schedule": float64(1)}
	somedayProject := map[string]any{"Type": float64(1), "Schedule": float64(2)}
	futureProject := map[string]any{"Type": float64(1), "Schedule": float64(1), "ScheduledDate": "2026-06-10T00:00:00Z"}

	tests := []struct {
		name    string
		task    map[string]any
		project map[string]any
		want    string
	}{
		{name: "inbox", task: map[string]any{"Schedule": float64(0)}, want: "inbox"},
		{name: "started today stays in anytime", task: map[string]any{"Schedule": float64(1), "ScheduledDate": today + "T00:00:00Z"}, want: "anytime"},
		{name: "started earlier stays in anytime", task: map[string]any{"Schedule": float64(1), "ScheduledDate": "2026-06-03T00:00:00Z"}, want: "anytime"},
		{name: "anytime", task: map[string]any{"Schedule": float64(1), "ScheduledDate": ""}, want: "anytime"},
		{name: "anytime in active project", task: map[string]any{"Schedule": float64(1)}, project: activeProject, want: "anytime"},
		{name: "anytime in someday project is hidden", task: map[string]any{"Schedule": float64(1)}, project: somedayProject, want: ""},
		{name: "anytime in future project is hidden", task: map[string]any{"Schedule": float64(1)}, project: futureProject, want: ""},
		{name: "upcoming", task: map[string]any{"Schedule": float64(2), "ScheduledDate": "2026-06-05T00:00:00Z"}, want: "upcoming"},
		{name: "someday", task: map[string]any{"Schedule": float64(2), "ScheduledDate": ""}, want: "someday"},
		{name: "someday in project is not listed", task: map[string]any{"Schedule": float64(2)}, project: activeProject, want: ""},
		{name: "repeating template is upcoming", task: map[string]any{"Schedule": float64(2), "IsRepeatingTemplate": true}, want: "upcoming"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstString(stateViews(tt.task, tt.project, today))
			if got != tt.want {
				t.Fatalf("stateViews() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanNoteRemovesThingsXMLAndKeepsLinks(t *testing.T) {
	got := cleanNote(`<note xml:space="preserve">Read &amp; file<br/>https://example.com?a=1&amp;b=2</note>`)
	want := "Read & file\nhttps://example.com?a=1&b=2"
	if got != want {
		t.Fatalf("cleanNote() = %q, want %q", got, want)
	}
}

func TestApplyCLITodayReplacesCacheTodayAndKeepsCLIOrder(t *testing.T) {
	model := map[string]any{
		"tasks": []map[string]any{
			{
				"uuid":         "cache-today",
				"title":        "Cache Today",
				"views":        []string{"today"},
				"projectTitle": "Old Project",
			},
			{
				"uuid":         "task-2",
				"title":        "Task 2",
				"views":        []string{"anytime"},
				"projectTitle": "Project B",
			},
			{
				"uuid":         "task-1",
				"title":        "Task 1",
				"views":        []string{"anytime"},
				"projectTitle": "Project A",
			},
		},
		"lists": map[string][]map[string]any{
			"today": {
				{"uuid": "cache-today", "title": "Cache Today", "views": []string{"today"}},
			},
		},
	}

	applyCLIToday(model, []map[string]any{
		{"uuid": "task-1", "title": "Task 1"},
		{"uuid": "task-2", "title": "Task 2"},
	})

	lists := model["lists"].(map[string][]map[string]any)
	today := lists["today"]
	if len(today) != 2 {
		t.Fatalf("today length = %d, want 2", len(today))
	}
	if today[0]["uuid"] != "task-1" || today[1]["uuid"] != "task-2" {
		t.Fatalf("today order = %v, want CLI order task-1, task-2", today)
	}
	tasks := model["tasks"].([]map[string]any)
	for _, task := range tasks {
		if task["uuid"] == "cache-today" && firstString(asStringSlice(task["views"])) == "today" {
			t.Fatalf("cache-derived Today row still has today view: %v", task)
		}
	}
}

func TestApplyCLITodayEmptyResultClearsCacheToday(t *testing.T) {
	model := map[string]any{
		"tasks": []map[string]any{
			{"uuid": "project-task", "title": "Project Task", "todayIndex": 42, "views": []string{"anytime", "today"}},
		},
		"lists": map[string][]map[string]any{
			"today": {
				{"uuid": "project-task", "title": "Project Task"},
			},
		},
	}

	applyCLIToday(model, nil)

	today := model["lists"].(map[string][]map[string]any)["today"]
	if len(today) != 0 {
		t.Fatalf("today = %v, want empty CLI result to remain empty", today)
	}
	for _, task := range model["tasks"].([]map[string]any) {
		if task["uuid"] == "project-task" && firstString(asStringSlice(task["views"])) == "today" {
			t.Fatalf("cache-only project task kept today view: %v", task)
		}
	}
}

func TestIsOpenCacheItemRejectsCompletionDate(t *testing.T) {
	task := map[string]any{
		"Type":           float64(0),
		"Status":         float64(0),
		"InTrash":        false,
		"CompletionDate": "2026-06-01T12:00:00Z",
	}
	if isOpenCacheItem(task, 0) {
		t.Fatal("task with completion date was treated as open")
	}
}

func TestApplyCLITodayKeepsManuallyMarkedTaskWithOldDate(t *testing.T) {
	model := map[string]any{
		"tasks": []map[string]any{
			{"uuid": "manual", "title": "Manual Today", "views": []string{"anytime"}, "scheduledDate": "2020-01-01"},
		},
		"lists": map[string][]map[string]any{},
	}

	applyCLIToday(model, []map[string]any{
		{"uuid": "manual", "title": "Manual Today"},
	})

	today := model["lists"].(map[string][]map[string]any)["today"]
	if len(today) != 1 || today[0]["uuid"] != "manual" {
		t.Fatalf("today = %v, want manually marked task despite old stored date", today)
	}
}

func TestApplyVisibleTaskFilterRemovesCacheOnlyRowsAndRecounts(t *testing.T) {
	model := map[string]any{
		"tasks": []map[string]any{
			{"uuid": "visible", "title": "Visible", "project": "project-1", "area": "area-1", "views": []string{"anytime"}},
			{"uuid": "archived", "title": "Archived", "project": "project-1", "area": "area-1", "views": []string{"anytime", "today"}},
		},
		"lists": map[string][]map[string]any{
			"anytime": {
				{"uuid": "visible", "title": "Visible"},
				{"uuid": "archived", "title": "Archived"},
			},
			"today": {
				{"uuid": "archived", "title": "Archived"},
			},
		},
		"projects": []map[string]any{{"uuid": "project-1", "title": "Project", "count": 2}},
		"areas":    []map[string]any{{"uuid": "area-1", "title": "Area", "count": 2}},
	}

	applyVisibleTaskFilter(model, []map[string]any{{"uuid": "visible"}})

	tasks := model["tasks"].([]map[string]any)
	if len(tasks) != 1 || tasks[0]["uuid"] != "visible" {
		t.Fatalf("tasks = %v, want only visible", tasks)
	}
	lists := model["lists"].(map[string][]map[string]any)
	if len(lists["today"]) != 0 {
		t.Fatalf("today list = %v, want empty after filtering cache-only row", lists["today"])
	}
	projects := model["projects"].([]map[string]any)
	if len(projects) != 1 || int(asNumber(projects[0], "count")) != 1 {
		t.Fatalf("projects = %v, want one counted visible task", projects)
	}
	areas := model["areas"].([]map[string]any)
	if len(areas) != 1 || int(asNumber(areas[0], "count")) != 1 {
		t.Fatalf("areas = %v, want one counted visible task", areas)
	}
}

func TestAreaTagsReadsIDsAndObjects(t *testing.T) {
	tags := map[string]map[string]any{"T1": {"uuid": "T1", "title": "#coding"}, "T2": {"uuid": "T2", "title": "Home"}}
	area := map[string]any{"TagIDs": []any{"T1", "missing"}, "Tags": []any{map[string]any{"UUID": "T2"}, map[string]any{"Title": "Loose"}, "T1"}}
	ids, titles := areaTags(area, tags)
	if strings.Join(ids, ",") != "T1,T2" || strings.Join(titles, ",") != "#coding,Home,Loose" {
		t.Fatalf("areaTags = %q %q", ids, titles)
	}
	if ids, titles := areaTags(map[string]any{"Tags": nil}, tags); len(ids) != 0 || len(titles) != 0 {
		t.Fatalf("areaTags(no tags) = %v %v", ids, titles)
	}
}

func TestValidWhen(t *testing.T) {
	for _, value := range []string{"inbox", "today", "anytime", "someday"} {
		if !validWhen(value) {
			t.Fatalf("validWhen(%q) = false, want true", value)
		}
	}
	if validWhen("upcoming") {
		t.Fatal("validWhen(upcoming) = true, want false")
	}
}

func TestBasicAuthProtectsWhenConfigured(t *testing.T) {
	t.Setenv("APP_USERNAME", "admin")
	t.Setenv("APP_PASSWORD", "secret")

	protected := basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	protected.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("admin", "secret")
	protected.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized code = %d, want %d", authorized.Code, http.StatusNoContent)
	}
}

func TestSameOriginGuardBlocksCrossSiteWrites(t *testing.T) {
	protected := sameOriginGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	crossOrigin := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/create", nil)
	request.Host = "127.0.0.1:4173"
	request.Header.Set("Origin", "https://example.com")
	protected.ServeHTTP(crossOrigin, request)
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin code = %d, want %d", crossOrigin.Code, http.StatusForbidden)
	}

	crossReferer := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/create", nil)
	request.Host = "127.0.0.1:4173"
	request.Header.Set("Referer", "https://example.com/path")
	protected.ServeHTTP(crossReferer, request)
	if crossReferer.Code != http.StatusForbidden {
		t.Fatalf("cross-referer code = %d, want %d", crossReferer.Code, http.StatusForbidden)
	}

	sameOrigin := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/create", nil)
	request.Host = "127.0.0.1:4173"
	request.Header.Set("Origin", "http://127.0.0.1:4173")
	request.Header.Set("Content-Type", "application/json")
	protected.ServeHTTP(sameOrigin, request)
	if sameOrigin.Code != http.StatusNoContent {
		t.Fatalf("same-origin code = %d, want %d", sameOrigin.Code, http.StatusNoContent)
	}
}

func TestRejectedLoginDoesNotCreateSession(t *testing.T) {
	cli := filepath.Join(t.TempDir(), "things-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\necho 'invalid Things Cloud credentials' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)

	sessionMu.Lock()
	previousSession := session
	session = sessionState{}
	sessionMu.Unlock()
	t.Cleanup(func() {
		sessionMu.Lock()
		session = previousSession
		sessionMu.Unlock()
	})

	login := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"email":"wrong@example.com","password":"wrong-password"}`))
	login.Header.Set("content-type", "application/json")
	loginResponse := httptest.NewRecorder()
	apiHandler(loginResponse, login)
	if loginResponse.Code != http.StatusBadGateway {
		t.Fatalf("login code = %d, want %d", loginResponse.Code, http.StatusBadGateway)
	}

	statusResponse := httptest.NewRecorder()
	apiHandler(statusResponse, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	var status struct {
		SignedIn bool   `json:"signedIn"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(statusResponse.Body, 1_000_000)).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.SignedIn || status.Email != "" {
		t.Fatalf("status after rejected login = %+v, want signed out", status)
	}
}

func TestCachePathUsesDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("THINGS_CLI_CACHE", "")

	if got := cachePath(); got != dir+string(os.PathSeparator)+"things-cli-state-v8.json" {
		t.Fatalf("cachePath() = %q", got)
	}
}

func taskOrder(tasks []map[string]any) []string {
	uuids := make([]string, len(tasks))
	for i, task := range tasks {
		uuids[i] = asString(task, "uuid")
	}
	return uuids
}

func TestSortTasksMatchesThingsOrder(t *testing.T) {
	anytime := []map[string]any{
		{"uuid": "area-b-project", "area": "area-b", "areaIndex": 2, "project": "p3", "projectIndex": 1, "index": 1},
		{"uuid": "area-a-heading-2", "area": "area-a", "areaIndex": 1, "project": "p1", "projectIndex": 5, "heading": "h2", "headingIndex": 2, "index": 1},
		{"uuid": "area-a-heading-1", "area": "area-a", "areaIndex": 1, "project": "p1", "projectIndex": 5, "heading": "h1", "headingIndex": 1, "index": 9},
		{"uuid": "area-a-project-loose", "area": "area-a", "areaIndex": 1, "project": "p1", "projectIndex": 5, "index": 20},
		{"uuid": "area-a-earlier-project", "area": "area-a", "areaIndex": 1, "project": "p2", "projectIndex": -3, "index": 0},
		{"uuid": "area-a-loose", "area": "area-a", "areaIndex": 1, "index": 50},
		{"uuid": "loose-2", "index": 2},
		{"uuid": "loose-1", "index": 1},
	}
	sortTasks(anytime, "anytime")
	want := []string{"loose-1", "loose-2", "area-a-loose", "area-a-earlier-project", "area-a-project-loose", "area-a-heading-1", "area-a-heading-2", "area-b-project"}
	if got := taskOrder(anytime); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("anytime order = %v, want %v", got, want)
	}

	today := []map[string]any{
		{"uuid": "project-task", "area": "area-a", "areaIndex": 1, "project": "p1", "todayDate": "2026-10-16", "todayIndex": 1},
		{"uuid": "older-low", "todayDate": "2026-09-01", "todayIndex": -5},
		{"uuid": "newer-high", "todayDate": "2026-10-01", "todayIndex": 30},
		{"uuid": "newer-low", "todayDate": "2026-10-01", "todayIndex": -10},
	}
	sortTasks(today, "today")
	want = []string{"newer-low", "newer-high", "older-low", "project-task"}
	if got := taskOrder(today); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("today order = %v, want %v", got, want)
	}

	upcoming := []map[string]any{
		{"uuid": "template-without-date", "index": 0},
		{"uuid": "template-later", "todayDate": "2027-02-27"},
		{"uuid": "scheduled-soon", "scheduledDate": "2026-10-20"},
	}
	sortTasks(upcoming, "upcoming")
	want = []string{"scheduled-soon", "template-later", "template-without-date"}
	if got := taskOrder(upcoming); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("upcoming order = %v, want %v", got, want)
	}
}

// TestSyncModelUsesThingsCLI runs syncModel against a stand-in for things-cli
// and a cache in the format the patched things-cli writes.
func TestSyncModelUsesThingsCLI(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	cli := filepath.Join(dir, "things-cli")
	script := `#!/bin/sh
echo "$*" >> "` + calls + `"
case "$1" in
  list) echo '[{"uuid":"repeat","title":"Water plants","status":0,"schedule":1},{"uuid":"today","title":"Call","status":0,"schedule":1,"scheduledDate":"2026-10-15"}]' ;;
  today) echo '[{"uuid":"today","title":"Call","status":0,"schedule":1,"scheduledDate":"2026-10-15"}]' ;;
  *) echo "unexpected command $1" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "things-cli-state-v8.json")
	if err := os.WriteFile(cache, []byte(`{
  "version": 4, "historyId": "h", "serverIndex": 7,
  "state": {
    "Areas": {"area": {"UUID": "area", "Title": "Home", "Index": 5}},
    "Tasks": {
      "repeat": {"UUID": "repeat", "Title": "Water plants", "Status": 0, "Type": 0, "Schedule": 1, "AreaIDs": ["area"],
        "ScheduledDate": "2099-01-01T00:00:00Z", "IsRepeatingTemplate": true, "TodayIndexReferenceDate": "2099-01-01T00:00:00Z",
        "RepeatRule": {"ed":64092211200,"fa":1,"fu":256,"ia":4070908800,"of":[{"wd":5}],"rc":0,"rrv":4,"sr":4070908800,"tp":0,"ts":0}},
      "today": {"UUID": "today", "Title": "Call", "Status": 0, "Type": 0, "Schedule": 1, "ScheduledDate": "2026-10-15T00:00:00Z",
        "TodayIndex": 3, "TodayIndexReferenceDate": "2026-10-15T00:00:00Z", "Evening": true},
      "closed": {"UUID": "closed", "Title": "Not listed by the CLI", "Status": 0, "Type": 0, "Schedule": 1}
    },
    "Tags": {}, "CheckListItems": {}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("THINGS_CLI_CACHE", cache)
	sessionMu.Lock()
	previousSession := session
	session = sessionState{Email: "user@example.com", Password: "secret"}
	sessionMu.Unlock()
	t.Cleanup(func() {
		sessionMu.Lock()
		session = previousSession
		sessionMu.Unlock()
	})

	model, err := syncModel()
	if err != nil {
		t.Fatalf("syncModel: %v", err)
	}
	called, _ := os.ReadFile(calls)
	if got := strings.Fields(strings.ReplaceAll(string(called), "\n", " ")); strings.Join(got, ",") != "list,today" {
		t.Fatalf("CLI calls = %q, want list then today without extra flags", called)
	}
	tasks := model["tasks"].([]map[string]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks = %v, want only the two tasks things-cli lists", tasks)
	}
	lists := model["lists"].(map[string][]map[string]any)
	if len(lists["upcoming"]) != 1 || lists["upcoming"][0]["uuid"] != "repeat" || len(lists["anytime"]) != 1 || lists["anytime"][0]["uuid"] != "today" {
		t.Fatalf("repeating template not in Upcoming only: upcoming=%v anytime=%v", lists["upcoming"], lists["anytime"])
	}
	if len(lists["today"]) != 1 || lists["today"][0]["uuid"] != "today" || lists["today"][0]["todayDate"] != "2026-10-15" {
		t.Fatalf("today = %v", lists["today"])
	}
	if today := lists["today"][0]; today["evening"] != true || today["repeat"] != "" {
		t.Fatalf("today task evening=%v repeat=%v, want This Evening and no repeat", today["evening"], today["repeat"])
	}
	if repeat := lists["upcoming"][0]; repeat["evening"] != false || repeat["repeat"] != "Every week on Friday" {
		t.Fatalf("template evening=%v repeat=%v", repeat["evening"], repeat["repeat"])
	}
	areas := model["areas"].([]map[string]any)
	if len(areas) != 1 || areas[0]["index"] != 5 {
		t.Fatalf("areas = %v, want Home with index 5", areas)
	}
}

func TestValuesSortedKeepsEmptyCollections(t *testing.T) {
	got := valuesSorted(map[string]map[string]any{
		"a": {"uuid": "a", "title": "Busy", "count": 2},
		"b": {"uuid": "b", "title": "Empty project", "count": 0},
	})
	if len(got) != 2 {
		t.Fatalf("valuesSorted = %v, want the empty project kept", got)
	}
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	cli := filepath.Join(dir, "things-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\ntouch "+marker+"\necho '{}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("APP_READ_ONLY", "1")
	sessionMu.Lock()
	previousSession := session
	session = sessionState{Email: "user@example.com", Password: "secret"}
	sessionMu.Unlock()
	t.Cleanup(func() {
		sessionMu.Lock()
		session = previousSession
		sessionMu.Unlock()
	})

	for path, body := range map[string]string{
		"/api/complete": `{"uuid":"BwG112abcdefghijkLMNOP"}`,
		"/api/create":   `{"title":"Test","when":"inbox"}`,
		"/api/cancel":   `{"uuids":["BwG112abcdefghijkLMNOP"]}`,
	} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("content-type", "application/json")
		response := httptest.NewRecorder()
		apiHandler(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s code = %d, want 403", path, response.Code)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("things-cli ran although APP_READ_ONLY is set")
	}
}

func TestProjectWhenMatchesThingsLists(t *testing.T) {
	today := "2026-10-27"
	cases := []struct {
		project map[string]any
		want    string
	}{
		{map[string]any{"Schedule": 1}, "anytime"},
		{map[string]any{"Schedule": 1, "ScheduledDate": "2026-10-01T00:00:00Z"}, "anytime"},
		{map[string]any{"Schedule": 2}, "someday"},
		{map[string]any{"Schedule": 2, "ScheduledDate": "2026-11-02T00:00:00Z"}, "upcoming"},
		{map[string]any{"Schedule": 1, "ScheduledDate": "2026-10-28T00:00:00Z"}, "upcoming"},
	}
	for _, c := range cases {
		if got := projectWhen(c.project, today); got != c.want {
			t.Fatalf("projectWhen(%v) = %q, want %q", c.project, got, c.want)
		}
	}
}

func TestRecountKeepsEmptyProjectsAndAreas(t *testing.T) {
	model := map[string]any{
		"projects": []map[string]any{{"uuid": "p", "title": "P", "count": 3}},
		"areas":    []map[string]any{{"uuid": "a", "title": "A", "count": 3}},
	}
	recountCollections(model, nil)
	if projects := model["projects"].([]map[string]any); len(projects) != 1 || projects[0]["count"] != 0 {
		t.Fatalf("projects = %v, want the project kept with count 0", projects)
	}
	if areas := model["areas"].([]map[string]any); len(areas) != 1 || areas[0]["count"] != 0 {
		t.Fatalf("areas = %v, want the area kept with count 0", areas)
	}
}

// TestCreatePassesTypeAndStartDate checks the things-cli batch operations for
// new projects and dated to-dos, and that new to-dos start at the top.
func TestCreatePassesTypeAndStartDate(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	cli := filepath.Join(dir, "things-cli")
	script := "#!/bin/sh\n[ \"$1\" = batch ] || exit 2\ncat >> " + calls + "\necho >> " + calls + "\necho '{\"status\":\"ok\",\"results\":[{\"cmd\":\"create\",\"uuid\":\"NewUuid1234567890abcde\"}]}'\n"
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("APP_READ_ONLY", "")
	previousModels := models
	built := false
	models = newModelCache(func() (map[string]any, error) {
		if built {
			select {}
		}
		built = true
		return map[string]any{
			"tasks": []map[string]any{{"uuid": "a", "index": -100, "todayIndex": -40, "views": []string{"today"}}},
			"lists": map[string][]map[string]any{"today": {{"uuid": "a", "index": -100, "todayIndex": -40}}},
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

	post := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/create", strings.NewReader(body))
		request.Header.Set("content-type", "application/json")
		response := httptest.NewRecorder()
		apiHandler(response, request)
		return response
	}
	if response := post(`{"type":"project","title":"Garden","area":"AreaUuid1234567890abcd"}`); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "NewUuid1234567890abcde") {
		t.Fatalf("project = %d %s", response.Code, response.Body)
	}
	if response := post(`{"title":"Dentist","scheduled":"2026-11-03"}`); response.Code != http.StatusOK {
		t.Fatalf("scheduled = %d %s", response.Code, response.Body)
	}
	response := post(`{"title":"Call mum","when":"today"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Call mum"`) {
		t.Fatalf("today = %d %s", response.Code, response.Body)
	}
	// The new to-do is in the cached model at once, at the top of Today, so
	// it can be edited before the next sync.
	models.mu.Lock()
	today := models.model["lists"].(map[string][]map[string]any)["today"]
	models.mu.Unlock()
	if len(today) == 0 || today[0]["title"] != "Call mum" || today[0]["todayIndex"] != -552 {
		t.Fatalf("cached today = %v", today)
	}
	if response := post(`{"type":"heading","title":"Küche","project":"ProjMoveggggggggggggggg"}`); response.Code != http.StatusOK {
		t.Fatalf("heading = %d %s", response.Code, response.Body)
	}
	for _, bad := range []string{
		`{"type":"heading","title":"x"}`,
		`{"title":"x","scheduled":"03.11.2026"}`,
		`{"type":"project","title":"x","project":"ProjUuid1234567890abcd"}`,
		`{"type":"area","title":"x"}`,
	} {
		if response := post(bad); response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", bad, response.Code)
		}
	}
	// Placed below a selected to-do by the page: not moved to the top.
	if response := post(`{"title":"Below","when":"anytime","placed":true}`); response.Code != http.StatusOK {
		t.Fatalf("placed = %d %s", response.Code, response.Body)
	}
	// Creates are written at position 0 and answered; the moves to the top
	// follow in the background.
	want := []string{
		`[{"area":"AreaUuid1234567890abcd","cmd":"create","title":"Garden","type":"project","when":"anytime"}]`,
		`[{"cmd":"create","extra":{"scheduled":"2026-11-03"},"title":"Dentist"}]`,
		`[{"cmd":"create","title":"Call mum","when":"today"}]`,
		`[{"cmd":"create","project":"ProjMoveggggggggggggggg","title":"Küche","type":"heading","when":"anytime"}]`,
		`[{"cmd":"create","title":"Below","when":"anytime"}]`,
		`[{"cmd":"reorder","index":-612,"todayIndex":-552,"uuid":"NewUuid1234567890abcde"}]`,
		`[{"cmd":"reorder","index":-612,"uuid":"NewUuid1234567890abcde"}]`,
	}
	var lines []string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		got, _ := os.ReadFile(calls)
		lines = strings.Split(strings.TrimSpace(string(got)), "\n")
		if len(lines) >= len(want) {
			break
		}
	}
	var creates []string
	for _, line := range lines {
		if strings.Contains(line, `"cmd":"create"`) {
			creates = append(creates, line)
		}
	}
	sortedGot, sortedWant := slices.Clone(lines), slices.Clone(want)
	slices.Sort(sortedGot)
	slices.Sort(sortedWant)
	if !slices.Equal(sortedGot, sortedWant) || !slices.Equal(creates, want[:5]) {
		t.Fatalf("batch input =\n%s\nwant (moves in any order)\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestLogbookRuleKeepsTodaysCompletedUntilLogged(t *testing.T) {
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	done := func(completed string) map[string]any {
		return map[string]any{"Type": 0, "Status": 3, "CompletionDate": completed}
	}
	one := 1
	daily := newLogbookRule("2026-10-27T09:00:00Z", &one, now)
	if !daily.stillListed(done("2026-10-27T10:00:00Z")) {
		t.Fatal("to-do completed today after the last log is hidden")
	}
	if daily.stillListed(done("2026-10-27T08:00:00Z")) {
		t.Fatal("to-do completed before Log Completed is still listed")
	}
	if daily.stillListed(done("2026-10-26T23:00:00Z")) {
		t.Fatal("to-do completed yesterday is still listed with daily logging")
	}
	if daily.stillListed(map[string]any{"Type": 0, "Status": 3, "CompletionDate": "2026-10-27T10:00:00Z", "InTrash": true}) {
		t.Fatal("trashed to-do listed")
	}
	two, zero := 2, 0
	if !newLogbookRule("2026-10-01T00:00:00Z", &two, now).stillListed(done("2026-10-20T10:00:00Z")) {
		t.Fatal("manual logging hides an unlogged to-do")
	}
	if newLogbookRule("", &zero, now).stillListed(done("2026-10-27T10:00:00Z")) {
		t.Fatal("immediate logging keeps a completed to-do")
	}
	// Without settings: daily.
	if !newLogbookRule("", nil, now).stillListed(done("2026-10-27T10:00:00Z")) {
		t.Fatal("default rule hides today's completed to-do")
	}
}

func TestCompletedTodayStaysInTodayAfterCLIToday(t *testing.T) {
	done := map[string]any{"uuid": "done", "title": "Done", "status": "completed", "views": []string{"anytime", "today"}}
	open := map[string]any{"uuid": "open", "title": "Open", "status": "open", "views": []string{"anytime", "today"}}
	model := map[string]any{
		"tasks": []map[string]any{done, open},
		"lists": map[string][]map[string]any{"today": {done, open}},
	}
	applyVisibleTaskFilter(model, []map[string]any{{"uuid": "open"}})
	applyCLIToday(model, []map[string]any{{"uuid": "open"}})
	today := model["lists"].(map[string][]map[string]any)["today"]
	if len(today) != 2 {
		t.Fatalf("today = %v, want the completed to-do kept", today)
	}
}
