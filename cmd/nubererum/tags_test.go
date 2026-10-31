package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestTagRenameAndDeletePatchTheModel(t *testing.T) {
	post, calls := editHarness(t)
	writeProjectCache(t)
	projectModel(t)
	if _, err := models.cached(); err != nil {
		t.Fatal(err)
	}
	const tag = "TagOneeeeeeeeeeeeeeeee"
	if response := post("/api/tag-rename", `{"uuid":"`+tag+`","title":" Shop "}`); response.Code != http.StatusOK {
		t.Fatalf("rename = %d %s", response.Code, response.Body)
	}
	if !strings.Contains(calls(), `batch [{"cmd":"tag-rename","title":"Shop","uuid":"`+tag+`"}]`) {
		t.Fatalf("calls = %s", calls())
	}
	result, _ := models.cached()
	project := func() map[string]any {
		for _, row := range result.model["projects"].([]map[string]any) {
			if asString(row, "uuid") == projOpen {
				return row
			}
		}
		return nil
	}
	if tags := result.model["tags"].([]map[string]any); len(tags) != 1 || tags[0]["title"] != "Shop" {
		t.Fatalf("tags = %v", tags)
	}
	if got := asStringSlice(project()["tags"]); len(got) != 1 || got[0] != "Shop" {
		t.Fatalf("project tags = %v", got)
	}
	area := func() map[string]any { return result.model["areas"].([]map[string]any)[0] }
	if got := asStringSlice(area()["tags"]); len(got) != 1 || got[0] != "Shop" {
		t.Fatalf("area tags = %v", got)
	}

	if response := post("/api/tag-delete", `{"uuid":"`+tag+`"}`); response.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", response.Code, response.Body)
	}
	if !strings.Contains(calls(), `batch [{"cmd":"tag-delete","uuid":"`+tag+`"}]`) {
		t.Fatalf("calls = %s", calls())
	}
	result, _ = models.cached()
	if tags := result.model["tags"].([]map[string]any); len(tags) != 0 {
		t.Fatalf("tags = %v", tags)
	}
	if p := project(); len(asStringSlice(p["tags"])) != 0 || len(asStringSlice(p["tagIDs"])) != 0 {
		t.Fatalf("project after delete = %v", p)
	}
	if a := area(); len(asStringSlice(a["tags"])) != 0 || len(asStringSlice(a["tagIDs"])) != 0 {
		t.Fatalf("area after delete = %v", a)
	}
	for _, body := range []string{`{"uuid":"` + tag + `","title":" "}`, `{"uuid":"--x","title":"A"}`} {
		if response := post("/api/tag-rename", body); response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", body, response.Code)
		}
	}
	t.Setenv("APP_READ_ONLY", "1")
	for _, path := range []string{"/api/tag-rename", "/api/tag-delete"} {
		if response := post(path, `{}`); response.Code != http.StatusForbidden {
			t.Fatalf("read-only %s = %d, want 403", path, response.Code)
		}
	}
}
