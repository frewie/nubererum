package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPTokenManagementInTheWebApp(t *testing.T) {
	h := newMCPTestServer(t)
	call := func(method, path, body string) (int, map[string]any, string) {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		apiHandler(recorder, request)
		var data map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &data)
		return recorder.Code, data, recorder.Body.String()
	}

	if status, data, _ := call(http.MethodGet, "/api/mcp-tokens", ""); status != http.StatusOK || len(data["tokens"].([]any)) != 0 || data["path"] != "/mcp" {
		t.Fatalf("empty list = %d %v", status, data)
	}
	if status, data, _ := call(http.MethodPost, "/api/mcp-token-create", `{"name":"agent"}`); status != http.StatusBadRequest || !strings.Contains(data["error"].(string), "project or tag") {
		t.Fatalf("create without scope = %d %v", status, data)
	}
	if status, _, _ := call(http.MethodPost, "/api/mcp-token-create", `{"name":"bad name","tags":["code"]}`); status != http.StatusBadRequest {
		t.Fatalf("create with a bad name = %d", status)
	}
	if status, _, _ := call(http.MethodPost, "/api/mcp-token-create", `{"name":"agent","projects":["--x"]}`); status != http.StatusBadRequest {
		t.Fatalf("create with a bad project = %d", status)
	}

	status, data, _ := call(http.MethodPost, "/api/mcp-token-create", `{"name":"agent","tags":["#coding"],"projects":["`+mcpProjectA+`"]}`)
	value, _ := data["token"].(string)
	if status != http.StatusOK || !strings.HasPrefix(value, mcpTokenPrefix) {
		t.Fatalf("create = %d %v", status, data)
	}
	created := data["created"].(map[string]any)
	titles := []string{}
	for _, project := range created["covers"].([]any) {
		titles = append(titles, project.(map[string]any)["title"].(string))
	}
	if strings.Join(titles, ",") != "Alpha,Beta,Gamma" || len(created["areas"].([]any)) != 1 || created["readOnly"] != false {
		t.Fatalf("created token = %v", created)
	}
	if status, _, _ := call(http.MethodPost, "/api/mcp-token-create", `{"name":"agent","tags":["code"]}`); status != http.StatusBadRequest {
		t.Fatalf("duplicate name = %d", status)
	}

	// The new token works on /mcp at once.
	if status, _, message := h.tool(value, "list_projects", map[string]any{}); status != http.StatusOK {
		t.Fatalf("list_projects with the new token = %d %q", status, message)
	}
	status, data, raw := call(http.MethodGet, "/api/mcp-tokens", "")
	if status != http.StatusOK || len(data["tokens"].([]any)) != 1 || strings.Contains(raw, "hash") || strings.Contains(raw, value) {
		t.Fatalf("list = %d %s", status, raw)
	}
	if listed := data["tokens"].([]any)[0].(map[string]any); listed["lastUsedAt"] == nil || listed["name"] != "agent" {
		t.Fatalf("listed token = %v", listed)
	}

	if status, _, _ := call(http.MethodPost, "/api/mcp-token-remove", `{"name":"nobody"}`); status != http.StatusNotFound {
		t.Fatalf("remove unknown = %d", status)
	}
	if status, _, _ := call(http.MethodPost, "/api/mcp-token-remove", `{"name":"agent"}`); status != http.StatusOK {
		t.Fatalf("remove = %d", status)
	}
	if response := h.rpc(mcpRequestOptions{token: value}, "tools/list", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("removed token = %d, want 401", response.Code)
	}
}
