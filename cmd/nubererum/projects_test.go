package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	projOpen     = "ProjOpennnnnnnnnnnnnnn"
	projDone     = "ProjDonennnnnnnnnnnnnn"
	projLogged   = "ProjLoggedddddddddddddd"
	projHeading  = "ProjHeadinggggggggggggg"
	projToDo1    = "ProjToDoAaaaaaaaaaaaaa"
	projToDo2    = "ProjToDoBbbbbbbbbbbbbb"
	projToDoDone = "ProjToDoDoneeeeeeeeeee"
	projItemOpen = "ProjItemOpennnnnnnnnnn"
	projItemDone = "ProjItemDoneeeeeeeeeee"
	projArea     = "ProjAreaTaggeddddddddd"
)

// writeProjectCache writes a CLI cache with an open project (a heading, two
// open to-dos, a completed, a canceled and a trashed one), a project
// completed after the last log and one completed before it, and an area
// with a tag.
func writeProjectCache(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	// Completed now: a minute earlier can be yesterday just after midnight.
	recent := now.Format(time.RFC3339)
	old := now.Add(-72 * time.Hour).Format(time.RFC3339)
	logged := now.Add(-48 * time.Hour).Format(time.RFC3339)
	data := `{"version":8,"historyId":"h","serverIndex":1,"state":{
	  "Areas":{"` + projArea + `":{"UUID":"` + projArea + `","Title":"Garden","TagIDs":["TagOneeeeeeeeeeeeeeeee"]}},
	  "Tags":{"TagOneeeeeeeeeeeeeeeee":{"Title":"Errand"}},
	  "LogDate":"` + logged + `","LogInterval":1,
	  "Tasks":{
	    "` + projOpen + `":{"UUID":"` + projOpen + `","Title":"House","Type":1,"Status":0,"Schedule":1,"DeadlineDate":"2099-05-06T00:00:00Z","TagIDs":["TagOneeeeeeeeeeeeeeeee"]},
	    "` + projHeading + `":{"UUID":"` + projHeading + `","Title":"Kitchen","Type":2,"Status":0,"ParentTaskIDs":["` + projOpen + `"]},
	    "` + projToDo1 + `":{"UUID":"` + projToDo1 + `","Title":"Paint","Type":0,"Status":0,"Schedule":1,"ParentTaskIDs":["` + projOpen + `"]},
	    "` + projToDo2 + `":{"UUID":"` + projToDo2 + `","Title":"Sink","Type":0,"Status":0,"Schedule":1,"ActionGroupIDs":["` + projHeading + `"]},
	    "` + projToDoDone + `":{"UUID":"` + projToDoDone + `","Title":"Plan","Type":0,"Status":3,"CompletionDate":"` + old + `","ParentTaskIDs":["` + projOpen + `"]},
	    "canceled":{"UUID":"canceled","Title":"Skip","Type":0,"Status":2,"CompletionDate":"` + old + `","ParentTaskIDs":["` + projOpen + `"]},
	    "trashed":{"UUID":"trashed","Title":"Gone","Type":0,"Status":0,"InTrash":true,"ParentTaskIDs":["` + projOpen + `"]},
	    "` + projDone + `":{"UUID":"` + projDone + `","Title":"Garden","Type":1,"Status":3,"Schedule":1,"CompletionDate":"` + recent + `"},
	    "` + projLogged + `":{"UUID":"` + projLogged + `","Title":"Old","Type":1,"Status":3,"Schedule":1,"CompletionDate":"` + old + `"}
	  },
	  "CheckListItems":{
	    "` + projItemOpen + `":{"Title":"Brush","Status":0,"TaskIDs":["` + projToDo1 + `"]},
	    "` + projItemDone + `":{"Title":"Tape","Status":3,"TaskIDs":["` + projToDo1 + `"]}
	  }}}`
	cache := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(cache, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLI_CACHE", cache)
}

func TestProjectModelHasProgressStatusAndDetails(t *testing.T) {
	writeProjectCache(t)
	model, err := syncModelFromCache()
	if err != nil {
		t.Fatal(err)
	}
	projects := map[string]map[string]any{}
	for _, project := range model["projects"].([]map[string]any) {
		projects[asString(project, "uuid")] = project
	}
	open := projects[projOpen]
	// Paint, Sink (under the heading) and the completed Plan; not the
	// canceled or trashed ones.
	if open["total"] != 3 || open["done"] != 1 || open["status"] != "open" {
		t.Fatalf("open project = %v", open)
	}
	if open["deadline"] != "2099-05-06" || len(asStringSlice(open["tagIDs"])) != 1 || asStringSlice(open["tags"])[0] != "Errand" {
		t.Fatalf("open project details = %v", open)
	}
	if done := projects[projDone]; done == nil || done["status"] != "completed" {
		t.Fatalf("project completed after the last log = %v, want it listed as completed", done)
	}
	if _, ok := projects[projLogged]; ok {
		t.Fatal("a logged project is still listed")
	}
	if got := mcpModelProjects(model); len(got) != 1 || asString(got[0], "uuid") != projOpen {
		t.Fatalf("MCP projects = %v, want the open one only", got)
	}
}

// projectModel serves the model of the project cache to the write
// handlers; later syncs (started by writes) never finish.
func projectModel(t *testing.T) {
	t.Helper()
	built := false
	models = newModelCache(func() (map[string]any, error) {
		if built {
			select {}
		}
		built = true
		return syncModelFromCache()
	})
}

func TestProjectCompleteWritesToDosAndChecklistsBeforeTheProject(t *testing.T) {
	post, calls := editHarness(t)
	writeProjectCache(t)
	projectModel(t)

	if response := post("/api/project-complete", `{"uuid":"`+projOpen+`","mode":"complete","rest":"complete"}`); response.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", response.Code, response.Body)
	}
	want := `[{"cmd":"checklist-complete","done":true,"uuid":"` + projItemOpen + `"},` +
		`{"cmd":"complete","uuid":"` + projToDo1 + `"},{"cmd":"complete","uuid":"` + projToDo2 + `"},` +
		`{"cmd":"complete","uuid":"` + projOpen + `"}]`
	if !strings.Contains(calls(), want) {
		t.Fatalf("calls =\n%s\nwant %s", calls(), want)
	}
	result, _ := models.cached()
	for _, project := range result.model["projects"].([]map[string]any) {
		if asString(project, "uuid") == projOpen && (project["status"] != "completed" || asNumber(project, "done") != 3) {
			t.Fatalf("patched project = %v", project)
		}
	}
	for _, task := range result.model["tasks"].([]map[string]any) {
		if uuid := asString(task, "uuid"); (uuid == projToDo1 || uuid == projToDo2) && task["status"] != "completed" {
			t.Fatalf("patched to-do = %v", task)
		}
	}
	// It is completed now (in the patched model): a second try conflicts.
	if response := post("/api/project-complete", `{"uuid":"`+projOpen+`","mode":"complete"}`); response.Code != http.StatusConflict {
		t.Fatalf("second complete = %d, want 409", response.Code)
	}
}

func TestProjectCompleteCanCancelTheRest(t *testing.T) {
	post, calls := editHarness(t)
	writeProjectCache(t)
	projectModel(t)

	if response := post("/api/project-complete", `{"uuid":"`+projOpen+`","mode":"complete","rest":"cancel"}`); response.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", response.Code, response.Body)
	}
	want := `[{"cmd":"cancel","uuid":"` + projToDo1 + `"},{"cmd":"cancel","uuid":"` + projToDo2 + `"},{"cmd":"complete","uuid":"` + projOpen + `"}]`
	if !strings.Contains(calls(), want) {
		t.Fatalf("calls =\n%s\nwant %s (no checklist ticks when the rest is canceled)", calls(), want)
	}
	for body, code := range map[string]int{
		`{"uuid":"` + projOpen + `","mode":"finish"}`:                http.StatusBadRequest,
		`{"uuid":"` + projOpen + `","mode":"cancel","rest":"later"}`: http.StatusBadRequest,
		`{"uuid":"UnknownProjectttttttttt","mode":"cancel"}`:         http.StatusConflict,
		`{"uuid":"--x","mode":"cancel"}`:                             http.StatusBadRequest,
	} {
		if response := post("/api/project-complete", body); response.Code != code {
			t.Fatalf("%s = %d, want %d", body, response.Code, code)
		}
	}
}

func TestProjectScheduleDeadlineAndTags(t *testing.T) {
	post, calls := editHarness(t)
	writeProjectCache(t)
	projectModel(t)

	steps := []struct{ path, body, want string }{
		{"/api/schedule", `{"uuids":["` + projOpen + `"],"when":"someday"}`, `{"cmd":"schedule","uuid":"` + projOpen + `","when":"someday"}`},
		{"/api/schedule", `{"uuids":["` + projOpen + `"],"startDate":"2099-03-04"}`, `{"cmd":"schedule","startDate":"2099-03-04","uuid":"` + projOpen + `"}`},
		{"/api/deadline", `{"uuid":"` + projOpen + `","deadline":""}`, `{"cmd":"deadline","deadlineDate":"","uuid":"` + projOpen + `"}`},
		{"/api/tags", `{"uuid":"` + projOpen + `","tags":[]}`, `{"cmd":"tags","tags":[],"uuid":"` + projOpen + `"}`},
		// Areas carry tags too; Things writes them on the area.
		{"/api/tags", `{"uuid":"` + projArea + `","tags":[]}`, `{"cmd":"area-tags","tags":[],"uuid":"` + projArea + `"}`},
	}
	for _, step := range steps {
		if response := post(step.path, step.body); response.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", step.path, step.body, response.Code, response.Body)
		}
		if !strings.Contains(calls(), step.want) {
			t.Fatalf("calls =\n%s\nwant %s", calls(), step.want)
		}
	}
	result, _ := models.cached()
	for _, project := range result.model["projects"].([]map[string]any) {
		if asString(project, "uuid") == projOpen {
			if project["when"] != "upcoming" || project["scheduledDate"] != "2099-03-04" || project["deadline"] != "" || len(asStringSlice(project["tags"])) != 0 {
				t.Fatalf("patched project = %v", project)
			}
		}
	}
	if area := result.model["areas"].([]map[string]any)[0]; len(asStringSlice(area["tagIDs"])) != 0 || len(asStringSlice(area["tags"])) != 0 {
		t.Fatalf("patched area = %v", area)
	}
	for _, when := range []string{"inbox", "evening"} {
		if response := post("/api/schedule", `{"uuids":["`+projOpen+`"],"when":"`+when+`"}`); response.Code != http.StatusBadRequest {
			t.Fatalf("project to %s = %d, want 400", when, response.Code)
		}
	}
}

func TestHeadingRenameAndDeleteKeepTheToDos(t *testing.T) {
	post, calls := editHarness(t)
	writeProjectCache(t)
	projectModel(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}

	if response := post("/api/update", `{"uuid":"`+projHeading+`","title":"Bath"}`); response.Code != http.StatusOK {
		t.Fatalf("rename = %d %s", response.Code, response.Body)
	}
	if !strings.Contains(calls(), "edit|"+projHeading+"|--title|Bath|") {
		t.Fatalf("calls = %s", calls())
	}
	result, _ := models.cached()
	for _, task := range result.model["tasks"].([]map[string]any) {
		if asString(task, "uuid") == projToDo2 && task["headingTitle"] != "Bath" {
			t.Fatalf("to-do under the renamed heading = %v", task)
		}
	}

	if response := post("/api/heading-delete", `{"uuid":"`+projHeading+`"}`); response.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", response.Code, response.Body)
	}
	want := `[{"cmd":"move","project":"` + projOpen + `","uuid":"` + projToDo2 + `"},{"cmd":"delete","uuid":"` + projHeading + `"}]`
	if !strings.Contains(calls(), want) {
		t.Fatalf("calls =\n%s\nwant %s", calls(), want)
	}
	result, _ = models.cached()
	if headings, _ := result.model["headings"].([]map[string]any); len(headings) != 0 {
		t.Fatalf("headings = %v", headings)
	}
	for _, task := range result.model["tasks"].([]map[string]any) {
		if asString(task, "uuid") == projToDo2 && (task["heading"] != "" || task["project"] != projOpen) {
			t.Fatalf("to-do of the deleted heading = %v", task)
		}
	}
	if response := post("/api/heading-delete", `{"uuid":"`+projHeading+`"}`); response.Code != http.StatusConflict {
		t.Fatalf("second delete = %d, want 409", response.Code)
	}
}

func TestAreaDeleteTrashesItsContentThenTheArea(t *testing.T) {
	const (
		area     = "AreaHomeeeeeeeeeeeeeee"
		project  = "AreaProjecttttttttttttt"
		inside   = "AreaProjToDooooooooooo"
		direct   = "AreaToDoooooooooooooooo"
		done     = "AreaToDoDoneeeeeeeeeee"
		trashed  = "AreaToDoTrashhhhhhhhhh"
		elsewher = "OtherToDoooooooooooooo"
	)
	post, calls := editHarness(t)
	old := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339)
	data := `{"version":8,"historyId":"h","serverIndex":1,"state":{
	  "Areas":{"` + area + `":{"UUID":"` + area + `","Title":"Home"}},
	  "Tags":{},
	  "Tasks":{
	    "` + project + `":{"UUID":"` + project + `","Title":"House","Type":1,"Status":0,"Schedule":1,"AreaIDs":["` + area + `"]},
	    "` + inside + `":{"UUID":"` + inside + `","Title":"Paint","Type":0,"Status":0,"Schedule":1,"ParentTaskIDs":["` + project + `"]},
	    "` + direct + `":{"UUID":"` + direct + `","Title":"Water","Type":0,"Status":0,"Schedule":1,"AreaIDs":["` + area + `"]},
	    "` + done + `":{"UUID":"` + done + `","Title":"Mow","Type":0,"Status":3,"CompletionDate":"` + old + `","AreaIDs":["` + area + `"]},
	    "` + trashed + `":{"UUID":"` + trashed + `","Title":"Gone","Type":0,"Status":0,"InTrash":true,"AreaIDs":["` + area + `"]},
	    "` + elsewher + `":{"UUID":"` + elsewher + `","Title":"Call","Type":0,"Status":0,"Schedule":1}
	  },
	  "CheckListItems":{}}}`
	cache := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(cache, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLI_CACHE", cache)
	projectModel(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}

	response := post("/api/area-delete", `{"uuid":"`+area+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", response.Code, response.Body)
	}
	// The area's projects and to-dos (also completed ones), then the
	// project's to-dos, then the area; nothing already in the Trash.
	want := `[{"cmd":"trash","uuid":"` + project + `"},{"cmd":"trash","uuid":"` + done + `"},{"cmd":"trash","uuid":"` + direct + `"},` +
		`{"cmd":"trash","uuid":"` + inside + `"},{"cmd":"area-delete","uuid":"` + area + `"}]`
	if !strings.Contains(calls(), want) {
		t.Fatalf("calls =\n%s\nwant %s", calls(), want)
	}
	if !strings.Contains(response.Body.String(), `"trashed":["`+project+`","`+done+`","`+direct+`"]`) {
		t.Fatalf("response = %s", response.Body)
	}
	result, _ := models.cached()
	if areas := result.model["areas"].([]map[string]any); len(areas) != 0 {
		t.Fatalf("areas = %v", areas)
	}
	if projects := result.model["projects"].([]map[string]any); len(projects) != 0 {
		t.Fatalf("projects = %v", projects)
	}
	for _, task := range result.model["tasks"].([]map[string]any) {
		if id := asString(task, "uuid"); id != elsewher {
			t.Fatalf("to-do %s of the deleted area is still listed", id)
		}
	}
	if response := post("/api/area-delete", `{"uuid":"`+area+`"}`); response.Code != http.StatusConflict {
		t.Fatalf("second delete = %d, want 409", response.Code)
	}
	if response := post("/api/area-delete", `{"uuid":"--x"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid uuid = %d, want 400", response.Code)
	}
}

func TestEmptyTrashDeletesChildrenFirstInBatches(t *testing.T) {
	const (
		todo     = "TrashToDoooooooooooooooo"
		project  = "TrashProjecttttttttttttt"
		child    = "TrashChildddddddddddddd"
		heading  = "TrashHeadinggggggggggggg"
		under    = "TrashUnderrrrrrrrrrrrrr"
		keepToDo = "KeepToDoooooooooooooooo"
	)
	post, calls := editHarness(t)
	data := `{"version":8,"historyId":"h","serverIndex":1,"state":{"Areas":{},"Tags":{},"CheckListItems":{},
	  "Tasks":{
	    "` + todo + `":{"UUID":"` + todo + `","Title":"Old","Type":0,"InTrash":true},
	    "` + project + `":{"UUID":"` + project + `","Title":"House","Type":1,"InTrash":true},
	    "` + child + `":{"UUID":"` + child + `","Title":"Paint","Type":0,"ParentTaskIDs":["` + project + `"]},
	    "` + heading + `":{"UUID":"` + heading + `","Title":"Kitchen","Type":2,"ParentTaskIDs":["` + project + `"]},
	    "` + under + `":{"UUID":"` + under + `","Title":"Sink","Type":0,"ActionGroupIDs":["` + heading + `"]},
	    "` + keepToDo + `":{"UUID":"` + keepToDo + `","Title":"Call","Type":0}
	  }}}`
	cache := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(cache, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLI_CACHE", cache)
	previous := emptyTrashBatch
	emptyTrashBatch = 2
	t.Cleanup(func() { emptyTrashBatch = previous })

	response := post("/api/empty-trash", `{}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"deleted":5`) {
		t.Fatalf("empty trash = %d %s", response.Code, response.Body)
	}
	pair := func(id string) string {
		return `{"cmd":"delete","uuid":"` + id + `"},{"cmd":"purge","uuid":"` + id + `"}`
	}
	// To-dos, then headings, then projects; two items per write.
	want := "batch [" + pair(child) + "," + pair(todo) + "]\n" +
		"batch [" + pair(under) + "," + pair(heading) + "]\n" +
		"batch [" + pair(project) + "]\n"
	if got := calls(); !strings.Contains(got, want) || strings.Contains(got, keepToDo) {
		t.Fatalf("calls =\n%s\nwant\n%s", got, want)
	}
	t.Setenv("APP_READ_ONLY", "1")
	if response := post("/api/empty-trash", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read-only empty-trash = %d, want 403", response.Code)
	}
}

func TestRepeatCreatesATemplateAndTrashesTheOriginal(t *testing.T) {
	post, calls := editHarness(t)
	writeProjectCache(t)
	projectModel(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	start := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	response := post("/api/repeat", `{"uuid":"`+projToDo2+`","type":"fixed","unit":"week","every":2,"start":"`+start+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("repeat = %d %s", response.Code, response.Body)
	}
	// Title, project and heading go to the template, in the write that
	// trashes the original.
	want := `batch [{"cmd":"create","heading":"` + projHeading + `","project":"` + projOpen + `","repeat":{"every":2,"start":"` + start + `","type":"fixed","unit":"week"},"title":"Sink"},{"cmd":"trash","uuid":"` + projToDo2 + `"}]`
	if got := calls(); !strings.Contains(got, want) {
		t.Fatalf("calls =\n%s\nwant %s", got, want)
	}
	if !strings.Contains(response.Body.String(), `"uuid":"ItemNewwwwwwwwwwwwwwwww"`) {
		t.Fatalf("response = %s", response.Body)
	}
	result, _ := models.cached()
	for _, task := range result.model["tasks"].([]map[string]any) {
		if asString(task, "uuid") == projToDo2 {
			t.Fatal("the original is still listed")
		}
	}
	for name, body := range map[string]string{
		"past start":        `{"uuid":"` + projToDo1 + `","type":"fixed","unit":"day","every":1,"start":"2020-01-01"}`,
		"unit":              `{"uuid":"` + projToDo1 + `","type":"fixed","unit":"hour","every":1,"start":"` + start + `"}`,
		"type":              `{"uuid":"` + projToDo1 + `","type":"x","unit":"day","every":1,"start":"` + start + `"}`,
		"every":             `{"uuid":"` + projToDo1 + `","type":"fixed","unit":"day","every":100,"start":"` + start + `"}`,
		"month kind weekly": `{"uuid":"` + projToDo1 + `","type":"fixed","unit":"week","every":1,"month":"last","start":"` + start + `"}`,
	} {
		if response := post("/api/repeat", body); response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", name, response.Code)
		}
	}
	if response := post("/api/repeat", `{"uuid":"`+projToDoDone+`","type":"fixed","unit":"day","every":1,"start":"`+start+`"}`); response.Code != http.StatusConflict {
		t.Fatalf("completed to-do = %d, want 409", response.Code)
	}
	t.Setenv("APP_READ_ONLY", "1")
	if response := post("/api/repeat", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read-only repeat = %d, want 403", response.Code)
	}
}

// convertCLI answers every batch with one new id per operation (New0…,
// New1…, counted over all batches) and fails batch number failAt (from 1).
func convertCLI(t *testing.T, failAt int) (calls func() string) {
	t.Helper()
	dir := t.TempDir()
	log, count := filepath.Join(dir, "calls"), filepath.Join(dir, "count")
	script := `#!/bin/sh
input=$(cat)
printf 'batch %s\n' "$input" >> "` + log + `"
batches=$(( $(cat "` + count + `" 2>/dev/null || echo 0) + 1 ))
echo $batches > "` + count + `"
if [ $batches = ` + strconv.Itoa(failAt) + ` ]; then echo '{"error":"Things Cloud refused"}' >&2; exit 1; fi
n=$(printf '%s' "$input" | grep -o '"cmd":' | wc -l)
start=$(cat "` + count + `.ids" 2>/dev/null || echo 0)
out=""; i=0
while [ $i -lt $n ]; do
  out="$out$([ -n "$out" ] && echo ,){\"uuid\":\"New$(printf '%019d' $((start + i)))\"}"
  i=$((i + 1))
done
echo $((start + n)) > "` + count + `.ids"
echo "{\"status\":\"ok\",\"results\":[$out]}"
`
	cli := filepath.Join(dir, "things-cli")
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	return func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
}

// The checklist (Tape done, then Brush, as the model lists it) becomes the
// project's to-dos in that order.
func TestConvertToProjectCopiesTheToDoAndItsChecklist(t *testing.T) {
	post, _ := editHarness(t)
	calls := convertCLI(t, 0)
	writeProjectCache(t)
	projectModel(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	response := post("/api/convert-project", `{"uuid":"`+projToDo1+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("convert = %d %s", response.Code, response.Body)
	}
	project := "New" + strings.Repeat("0", 19)
	item := func(i int) string { return fmt.Sprintf("New%019d", i) }
	want := "batch [{\"cmd\":\"create\",\"title\":\"Paint\",\"type\":\"project\",\"when\":\"anytime\"}]\n" +
		"batch [{\"cmd\":\"create\",\"project\":\"" + project + "\",\"title\":\"Tape\",\"when\":\"anytime\"},{\"cmd\":\"create\",\"project\":\"" + project + "\",\"title\":\"Brush\",\"when\":\"anytime\"}]\n" +
		"batch [{\"cmd\":\"reorder\",\"index\":0,\"uuid\":\"" + item(1) + "\"},{\"cmd\":\"complete\",\"uuid\":\"" + item(1) + "\"},{\"cmd\":\"reorder\",\"index\":1,\"uuid\":\"" + item(2) + "\"},{\"cmd\":\"trash\",\"uuid\":\"" + projToDo1 + "\"}]\n"
	if got := calls(); got != want {
		t.Fatalf("calls =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(response.Body.String(), `"uuid":"`+project+`"`) || !strings.Contains(response.Body.String(), `"todos":2`) {
		t.Fatalf("response = %s", response.Body)
	}
}

func TestConvertToProjectStopsAtAFailedStep(t *testing.T) {
	post, _ := editHarness(t)
	calls := convertCLI(t, 2)
	writeProjectCache(t)
	projectModel(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	response := post("/api/convert-project", `{"uuid":"`+projToDo1+`"}`)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "The new project exists; the to-do is unchanged.") {
		t.Fatalf("convert = %d %s", response.Code, response.Body)
	}
	// The project was created, the to-dos failed, nothing was trashed.
	if got := calls(); strings.Count(got, "batch ") != 2 || strings.Contains(got, `"trash"`) {
		t.Fatalf("calls =\n%s", got)
	}
}
