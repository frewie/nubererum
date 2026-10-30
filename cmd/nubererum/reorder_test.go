package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func positions(t *testing.T, ops []reorderOp) string {
	t.Helper()
	parts := []string{}
	for _, op := range ops {
		value := 0
		switch {
		case op.Index != nil:
			value = *op.Index
		case op.TodayIndex != nil:
			value = *op.TodayIndex
		}
		part := op.UUID + "=" + itoa(value)
		if op.TodayDate != "" {
			part += "@" + op.TodayDate
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

func itoa(v int) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestPlanReorderUsesTheGapBetweenNeighbours(t *testing.T) {
	order := []reorderTask{{uuid: "a", position: 0}, {uuid: "c", position: 2000}, {uuid: "b", position: 1000}}
	ops, err := planReorder(order, "c", false)
	if err != nil || positions(t, ops) != "c=500" {
		t.Fatalf("ops = %s, %v; want c between a and b", positions(t, ops), err)
	}
	// To the top and to the end.
	if ops, _ := planReorder([]reorderTask{{uuid: "b", position: 9}, {uuid: "a", position: 4}}, "b", false); positions(t, ops) != "b=-508" {
		t.Fatalf("top = %s", positions(t, ops))
	}
	if ops, _ := planReorder([]reorderTask{{uuid: "b", position: 9}, {uuid: "a", position: 4}}, "a", false); positions(t, ops) != "a=521" {
		t.Fatalf("end = %s", positions(t, ops))
	}
}

func TestPlanReorderRenumbersWithoutRoom(t *testing.T) {
	order := []reorderTask{{uuid: "a", position: 10}, {uuid: "c", position: 12}, {uuid: "b", position: 11}, {uuid: "d", position: 13}}
	ops, err := planReorder(order, "c", false)
	if err != nil || positions(t, ops) != "c=522 b=1034 d=1546" {
		t.Fatalf("ops = %s, %v", positions(t, ops), err)
	}
}

func TestPlanReorderInTodayJoinsTheNeighboursDay(t *testing.T) {
	// Today sorts by the day a to-do entered Today (newest first), then ti.
	order := []reorderTask{
		{uuid: "new", position: -500, todayDate: "2026-10-20"},
		{uuid: "moved", position: 100, todayDate: "2026-08-01"},
		{uuid: "old", position: -50, todayDate: "2026-09-01"},
	}
	ops, err := planReorder(order, "moved", true)
	if err != nil || positions(t, ops) != "moved=12@2026-10-20" {
		t.Fatalf("ops = %s, %v; want moved after new, on its day", positions(t, ops), err)
	}
	// Within the same day only ti changes.
	order = []reorderTask{{uuid: "x", position: 0, todayDate: "2026-10-20"}, {uuid: "y", position: 900, todayDate: "2026-10-20"}, {uuid: "z", position: 400, todayDate: "2026-10-20"}}
	if ops, _ := planReorder(order, "y", true); positions(t, ops) != "y=200" {
		t.Fatalf("same day = %s", positions(t, ops))
	}
}

func TestReorderEndpointWritesOneBatch(t *testing.T) {
	dir := t.TempDir()
	stdin := filepath.Join(dir, "stdin")
	cli := filepath.Join(dir, "things-cli")
	script := "#!/bin/sh\n[ \"$1\" = batch ] || exit 2\ncat > " + stdin + "\necho '{\"status\":\"ok\"}'\n"
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("APP_READ_ONLY", "")
	previousModels := models
	models = newModelCache(func() (map[string]any, error) {
		return map[string]any{
			"tasks": []map[string]any{
				{"uuid": "TaskAaaaaaaaaaaaaaaaaaa", "index": 0},
				{"uuid": "TaskBbbbbbbbbbbbbbbbbbb", "index": 1000},
				{"uuid": "TaskCccccccccccccccccccc", "index": 2000},
			},
			"lists": map[string][]map[string]any{},
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
		request := httptest.NewRequest(http.MethodPost, "/api/reorder", strings.NewReader(body))
		request.Header.Set("content-type", "application/json")
		response := httptest.NewRecorder()
		apiHandler(response, request)
		return response
	}
	response := post(`{"view":"anytime","moved":"TaskCccccccccccccccccccc","order":["TaskAaaaaaaaaaaaaaaaaaa","TaskCccccccccccccccccccc","TaskBbbbbbbbbbbbbbbbbbb"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("reorder = %d %s", response.Code, response.Body)
	}
	written, _ := os.ReadFile(stdin)
	if string(written) != `[{"cmd":"reorder","uuid":"TaskCccccccccccccccccccc","index":500}]` {
		t.Fatalf("batch input = %s", written)
	}
	if response := post(`{"view":"anytime","moved":"TaskCccccccccccccccccccc","order":["TaskAaaaaaaaaaaaaaaaaaa","TaskCccccccccccccccccccc","Unknown1234567890abcdef"]}`); response.Code != http.StatusConflict {
		t.Fatalf("unknown task = %d, want 409", response.Code)
	}
	t.Setenv("APP_READ_ONLY", "1")
	if response := post(`{"view":"anytime","moved":"TaskCccccccccccccccccccc","order":["TaskAaaaaaaaaaaaaaaaaaa","TaskCccccccccccccccccccc"]}`); response.Code != http.StatusForbidden {
		t.Fatalf("read-only reorder = %d, want 403", response.Code)
	}
}

func TestReorderProjectsInTheSidebar(t *testing.T) {
	post, calls := editHarness(t)
	built := false
	models = newModelCache(func() (map[string]any, error) {
		if built {
			select {}
		}
		built = true
		return map[string]any{
			"tasks": []map[string]any{},
			"lists": map[string][]map[string]any{},
			"projects": []map[string]any{
				{"uuid": "ProjAaaaaaaaaaaaaaaaaaa", "index": -87474},
				{"uuid": "ProjBbbbbbbbbbbbbbbbbbb", "index": -87056},
				{"uuid": "ProjBikedddddddddddddd1", "index": 2034},
			},
		}, nil
	})
	response := post("/api/reorder", `{"view":"projects","moved":"ProjBikedddddddddddddd1","order":["ProjBikedddddddddddddd1","ProjAaaaaaaaaaaaaaaaaaa","ProjBbbbbbbbbbbbbbbbbbb"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("reorder = %d %s", response.Code, response.Body)
	}
	if got := calls(); !strings.Contains(got, `batch [{"cmd":"reorder","uuid":"ProjBikedddddddddddddd1","index":-87986}]`) {
		t.Fatalf("calls = %s", got)
	}
	models.mu.Lock()
	defer models.mu.Unlock()
	for _, project := range models.model["projects"].([]map[string]any) {
		if project["uuid"] == "ProjBikedddddddddddddd1" && project["index"] != -87986 {
			t.Fatalf("cached project index = %v", project["index"])
		}
	}
}

func TestProjectAreaAndAreaReorder(t *testing.T) {
	post, calls := editHarness(t)
	built := false
	models = newModelCache(func() (map[string]any, error) {
		if built {
			select {}
		}
		built = true
		return map[string]any{
			"tasks":    []map[string]any{},
			"lists":    map[string][]map[string]any{},
			"projects": []map[string]any{{"uuid": "ProjBikedddddddddddddd1", "title": "Bike", "area": "AreaPrivateeeeeeeeeee1", "index": 5}},
			"areas": []map[string]any{
				{"uuid": "AreaPrivateeeeeeeeeee1", "title": "Private", "index": -6500},
				{"uuid": "AreaStuddddddddddddddd1", "title": "Study", "index": -6071},
			},
		}, nil
	})
	if response := post("/api/project-area", `{"uuid":"ProjBikedddddddddddddd1","area":"AreaStuddddddddddddddd1"}`); response.Code != http.StatusOK {
		t.Fatalf("project-area = %d %s", response.Code, response.Body)
	}
	if response := post("/api/project-area", `{"uuid":"ProjBikedddddddddddddd1","area":""}`); response.Code != http.StatusOK {
		t.Fatalf("unfile = %d", response.Code)
	}
	if response := post("/api/reorder", `{"view":"areas","moved":"AreaStuddddddddddddddd1","order":["AreaStuddddddddddddddd1","AreaPrivateeeeeeeeeee1"]}`); response.Code != http.StatusOK {
		t.Fatalf("area reorder = %d %s", response.Code, response.Body)
	}
	got := calls()
	for _, want := range []string{
		`{"area":"AreaStuddddddddddddddd1","cmd":"move","uuid":"ProjBikedddddddddddddd1"}`,
		`{"cmd":"unfile","uuid":"ProjBikedddddddddddddd1"}`,
		`{"cmd":"area-reorder","uuid":"AreaStuddddddddddddddd1","index":-7012}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("calls =\n%s\nwant %s", got, want)
		}
	}
	if response := post("/api/project-area", `{"uuid":"ProjGoneeeeeeeeeeeeeee1","area":""}`); response.Code != http.StatusConflict {
		t.Fatalf("unknown project = %d, want 409", response.Code)
	}
}
