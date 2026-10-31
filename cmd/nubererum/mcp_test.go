package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	mcpProjectA = "ProjectAaaaaaaaaaaaaaaa"
	mcpProjectB = "ProjectBbbbbbbbbbbbbbbb"
	mcpHeadingA = "HeadingAaaaaaaaaaaaaaaa"
	mcpTagCode  = "TagCodeeeeeeeeeeeeeeeee"
	mcpTagHash  = "TagHashhhhhhhhhhhhhhhhh"
	mcpAreaWork = "AreaWorkkkkkkkkkkkkkkkk"
	mcpAreaHome = "AreaHomeeeeeeeeeeeeeeee"
	mcpProjectC = "ProjectCccccccccccccccc"
	mcpNewUUID1 = "Newwwwwwwwwwwwwwwwwwww1"
	mcpNewUUID2 = "Newwwwwwwwwwwwwwwwwwww2"
	mcpNewUUID3 = "Newwwwwwwwwwwwwwwwwwww3"
	mcpItemA    = "ItemAaaaaaaaaaaaaaaaaaa"
	mcpItemB    = "ItemBbbbbbbbbbbbbbbbbbb"
	mcpTaskA1   = "TaskA1aaaaaaaaaaaaaaaaa"
	mcpTaskA2   = "TaskA2aaaaaaaaaaaaaaaaa"
	mcpTaskA3   = "TaskA3aaaaaaaaaaaaaaaaa"
	mcpTaskB1   = "TaskB1bbbbbbbbbbbbbbbbb"
)

// mcpTestServer fakes things-cli and the model: two projects, Alpha (A)
// with an open to-do, one under a heading and one completed, and Beta (B).
// The model builder marks to-dos the fake CLI completed, so syncs started by
// writes agree with the patched cached model.
type mcpTestServer struct {
	t       *testing.T
	dir     string
	server  *mcpServer
	handler http.Handler
	logPath string
}

func newMCPTestServer(t *testing.T) *mcpTestServer {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	cli := filepath.Join(dir, "things-cli")
	script := `#!/bin/sh
if [ "$1" = batch ]; then
  input=$(cat)
  printf 'batch %s\n' "$input" >> "` + logPath + `"
  # Things Cloud takes one write per item and commit.
  # (plain sh, so the tests also run in golang:1.26-alpine without python3)
  if [ -n "$(printf '%s' "$input" | grep -o '"uuid": *"[^"]*"' | sort | uniq -d)" ]; then
    echo 'duplicate UUID in one commit - split into separate Write calls' >&2; exit 1
  fi
  echo '{"status":"ok","results":[{"uuid":"` + mcpNewUUID1 + `"},{"uuid":"` + mcpNewUUID2 + `"},{"uuid":"` + mcpNewUUID3 + `"}]}'
elif [ "$1" = create-tag ]; then
  printf '%s|' "$@" >> "` + logPath + `"; echo >> "` + logPath + `"
  echo '{"uuid":"` + mcpNewUUID3 + `"}'
else
  printf '%s|' "$@" >> "` + logPath + `"; echo >> "` + logPath + `"
  echo '{"status":"updated"}'
fi
`
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("APP_READ_ONLY", "")
	t.Setenv("APP_PUBLIC_ORIGINS", "")
	t.Setenv("DATA_DIR", dir)
	previousModels := models
	models = newModelCache(func() (map[string]any, error) {
		calls, _ := os.ReadFile(logPath)
		completed := map[string]bool{}
		edited := map[string]map[string]string{}
		created := []map[string]any{}
		createdProjects := []map[string]any{}
		// A1's checklist, with the fake CLI's checklist writes replayed.
		checklistA1 := []map[string]any{{"uuid": mcpItemA, "title": "Unit", "done": true, "index": 0}, {"uuid": mcpItemB, "title": "E2E", "done": false, "index": 1}}
		findItem := func(uuid string) int {
			for i, item := range checklistA1 {
				if item["uuid"] == uuid {
					return i
				}
			}
			return -1
		}
		fakeIDs := []string{mcpNewUUID1, mcpNewUUID2, mcpNewUUID3}
		for _, line := range strings.Split(string(calls), "\n") {
			if strings.HasPrefix(line, "complete|") {
				completed[strings.TrimSuffix(strings.TrimPrefix(line, "complete|"), "|")] = true
			}
			if strings.HasPrefix(line, "edit|") {
				// edit|<uuid>|--title|<title>|--note|<note>|
				parts := strings.Split(strings.TrimSuffix(line, "|"), "|")
				fields := map[string]string{}
				for i := 2; i+1 < len(parts); i += 2 {
					fields[strings.TrimPrefix(parts[i], "--")] = parts[i+1]
				}
				edited[parts[1]] = fields
			}
			if strings.HasPrefix(line, "batch ") {
				var ops []map[string]any
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "batch ")), &ops)
				for i, op := range ops {
					at := -1
					if uuid := asString(op, "uuid"); uuid != "" {
						at = findItem(uuid)
					}
					switch {
					case op["cmd"] == "checklist-add" && op["task"] == mcpTaskA1 && i < len(fakeIDs):
						checklistA1 = append(checklistA1, map[string]any{"uuid": fakeIDs[i], "title": op["title"], "done": false, "index": int(asNumber(op, "index"))})
					case op["cmd"] == "checklist-edit" && at >= 0:
						checklistA1[at]["title"] = op["title"]
					case op["cmd"] == "checklist-complete" && at >= 0:
						checklistA1[at]["done"] = op["done"] == true
					case op["cmd"] == "checklist-delete" && at >= 0:
						checklistA1 = append(checklistA1[:at], checklistA1[at+1:]...)
					case op["cmd"] == "checklist-reorder" && at >= 0:
						checklistA1[at]["index"] = int(asNumber(op, "index"))
					}
					if op["cmd"] == "create" && op["type"] == "project" {
						createdProjects = append(createdProjects, map[string]any{"uuid": mcpNewUUID1, "title": op["title"], "when": asString(op, "when"), "count": 0, "tags": []string{}, "area": asString(op, "area")})
					} else if op["cmd"] == "create" {
						// The fake CLI reports mcpNewUUID1 for the first result.
						created = append(created, map[string]any{"uuid": mcpNewUUID1, "title": op["title"], "note": asString(op, "note"), "status": "open", "project": asString(op, "project"), "heading": asString(op, "heading"), "headingTitle": "", "schedule": "anytime", "scheduledDate": "", "deadline": "", "tags": []string{}, "index": 9999, "checklist": []map[string]any{}})
					}
				}
			}
		}
		sort.SliceStable(checklistA1, func(i, j int) bool { return asNumber(checklistA1[i], "index") < asNumber(checklistA1[j], "index") })
		status := func(uuid, initial string) string {
			if completed[uuid] {
				return "completed"
			}
			return initial
		}
		tasks := []map[string]any{
			{"uuid": mcpTaskA1, "title": "Write tests", "note": "See <note xml>", "status": status(mcpTaskA1, "open"), "project": mcpProjectA, "heading": "", "headingTitle": "", "schedule": "anytime", "scheduledDate": "", "deadline": "2026-11-01", "tags": []string{"code"}, "index": 2,
				"checklist": checklistA1},
			{"uuid": mcpTaskA2, "title": "Deploy", "note": "", "status": status(mcpTaskA2, "open"), "project": mcpProjectA, "heading": mcpHeadingA, "headingTitle": "Phase 2", "schedule": "upcoming", "scheduledDate": "2026-11-03", "deadline": "", "tags": []string{}, "index": 1},
			{"uuid": mcpTaskA3, "title": "Old", "note": "", "status": "completed", "project": mcpProjectA, "heading": "", "headingTitle": "", "schedule": "anytime", "scheduledDate": "", "deadline": "", "tags": []string{}, "index": 0},
			{"uuid": mcpTaskB1, "title": "Secret", "note": "", "status": status(mcpTaskB1, "open"), "project": mcpProjectB, "heading": "", "headingTitle": "", "schedule": "anytime", "scheduledDate": "", "deadline": "", "tags": []string{}, "index": 0},
		}
		tasks = append(tasks, created...)
		for _, task := range tasks {
			for field, value := range edited[asString(task, "uuid")] {
				task[field] = value
			}
		}
		return map[string]any{
			"tasks":    tasks,
			"lists":    map[string][]map[string]any{"anytime": tasks},
			"headings": []map[string]any{{"uuid": mcpHeadingA, "title": "Phase 2", "project": mcpProjectA, "index": 0}},
			"tags":     []map[string]any{{"uuid": mcpTagCode, "title": "code", "index": 0}, {"uuid": "TagHomeeeeeeeeeeeeeeeee", "title": "Home", "index": 1}, {"uuid": mcpTagHash, "title": "#coding", "index": 2}},
			"projects": append([]map[string]any{
				{"uuid": mcpProjectA, "title": "Alpha", "when": "anytime", "count": 2, "tags": []string{}, "area": mcpAreaHome},
				{"uuid": mcpProjectB, "title": "Beta", "when": "anytime", "count": 1, "tags": []string{"Coding", "Home"}, "area": ""},
				{"uuid": mcpProjectC, "title": "Gamma", "when": "anytime", "count": 0, "tags": []string{}, "area": mcpAreaWork},
			}, createdProjects...),
			"areas": []map[string]any{
				{"uuid": mcpAreaWork, "title": "Work", "type": "area", "count": 1, "index": 0, "tags": []string{"#coding"}},
				{"uuid": mcpAreaHome, "title": "Home", "type": "area", "count": 1, "index": 1, "tags": []string{}},
			},
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
	server := newMCPServer(dir, newAuthLimiter())
	fallthroughHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	return &mcpTestServer{t: t, dir: dir, server: server, handler: server.route(fallthroughHandler), logPath: logPath}
}

func (h *mcpTestServer) createToken(name string, readOnly bool, projects ...string) string {
	h.t.Helper()
	token, err := h.server.store.create(name, projects, nil, readOnly, time.Now())
	if err != nil {
		h.t.Fatal(err)
	}
	return token
}

func (h *mcpTestServer) calls() string {
	data, _ := os.ReadFile(h.logPath)
	return string(data)
}

type mcpRequestOptions struct {
	token   string
	headers map[string]string
	raw     string
	method  string
}

func (h *mcpTestServer) rpc(options mcpRequestOptions, method string, params any) *httptest.ResponseRecorder {
	h.t.Helper()
	body := options.raw
	if body == "" {
		message := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
		if params != nil {
			message["params"] = params
		}
		data, _ := json.Marshal(message)
		body = string(data)
	}
	httpMethod := options.method
	if httpMethod == "" {
		httpMethod = http.MethodPost
	}
	request := httptest.NewRequest(httpMethod, "https://things.example.test/mcp", bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	if options.token != "" {
		request.Header.Set("Authorization", "Bearer "+options.token)
	}
	for key, value := range options.headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder
}

// tool calls a tool and returns the HTTP status with the structured result
// or the error message.
func (h *mcpTestServer) tool(token, name string, arguments map[string]any) (int, map[string]any, string) {
	h.t.Helper()
	response := h.rpc(mcpRequestOptions{token: token}, "tools/call", map[string]any{"name": name, "arguments": arguments})
	var envelope struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
			Content           []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		h.t.Fatalf("%s: %d %s", name, response.Code, response.Body)
	}
	if envelope.Error != nil {
		return response.Code, nil, envelope.Error.Message
	}
	if len(envelope.Result.Content) != 1 || envelope.Result.Content[0].Text == "" {
		h.t.Fatalf("%s: result without text content: %s", name, response.Body)
	}
	return response.Code, envelope.Result.StructuredContent, ""
}

func TestMCPTokenStoreHashesAndRemoves(t *testing.T) {
	dir := t.TempDir()
	store := newMCPTokenStore(dir)
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	token, err := store.create("laptop", []string{mcpProjectA, mcpProjectA, mcpProjectB}, nil, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, mcpTokenPrefix) || len(token) < 40 {
		t.Fatalf("token %q has no prefix or is too short", token)
	}
	raw, err := os.ReadFile(filepath.Join(dir, mcpTokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("token stored in plain text")
	}
	if info, _ := os.Stat(filepath.Join(dir, mcpTokenFileName)); info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %o, want 600", info.Mode().Perm())
	}
	found, ok := store.find(token)
	if !ok || found.Name != "laptop" || len(found.Projects) != 2 {
		t.Fatalf("find = %+v, %v", found, ok)
	}
	if _, ok := store.find(token[:len(token)-1] + "x"); ok {
		t.Fatal("altered token accepted")
	}
	if _, ok := store.find(strings.TrimPrefix(token, mcpTokenPrefix)); ok {
		t.Fatal("token without prefix accepted")
	}
	if _, err := store.create("laptop", []string{mcpProjectA}, nil, false, now); err == nil {
		t.Fatal("duplicate name accepted")
	}
	for _, bad := range []struct {
		name     string
		projects []string
		tags     []string
	}{{"", []string{mcpProjectA}, nil}, {"has space", []string{mcpProjectA}, nil}, {"ok", nil, nil}, {"ok", []string{"--not-an-id"}, nil}, {"ok", nil, []string{"--tag"}}, {"ok", nil, []string{"#"}}} {
		if _, err := store.create(bad.name, bad.projects, bad.tags, false, now); err == nil {
			t.Fatalf("create(%q, %v, %v) accepted", bad.name, bad.projects, bad.tags)
		}
	}
	tagged, err := store.create("tagged", nil, []string{"#Coding", "coding", " Home "}, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := store.find(tagged); strings.Join(found.Tags, ",") != "Coding,Home" || len(found.Projects) != 0 {
		t.Fatalf("tag token = %+v", found)
	}
	store.touch("laptop", now.Add(time.Hour))
	data, _ := store.load()
	if data.Tokens[0].LastUsedAt.IsZero() {
		t.Fatal("touch did not record the use")
	}
	if _, err := store.remove("laptop"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.find(token); ok {
		t.Fatal("removed token still accepted")
	}
	if _, err := store.remove("laptop"); err == nil {
		t.Fatal("removing a missing token succeeded")
	}
}

func TestMCPTokenCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("THINGS_CLI_CACHE", filepath.Join(dir, "none.json"))
	var stdout, stderr bytes.Buffer
	if code := runMCPTokenCommand([]string{"mcp-token-create", "--name", "repo-a", "--project", mcpProjectA, "--read-only"}, &stdout, &stderr); code != 0 {
		t.Fatalf("create exit %d: %s", code, stderr.String())
	}
	match := regexp.MustCompile(`tca_[A-Za-z0-9_-]+`).FindString(stdout.String())
	if match == "" || !strings.Contains(stdout.String(), "read-only") || !strings.Contains(stdout.String(), "claude mcp add") {
		t.Fatalf("create output: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), mcpProjectA+" (not in the current cache)") {
		t.Fatalf("expected the unknown project to be marked, got %q", stdout.String())
	}
	if token, ok := newMCPTokenStore(dir).find(match); !ok || !token.ReadOnly || token.Projects[0] != mcpProjectA {
		t.Fatalf("stored token = %+v, %v", token, ok)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runMCPTokenCommand([]string{"mcp-token-create", "--name", "repo-a", "--project", mcpProjectA}, &stdout, &stderr); code == 0 {
		t.Fatal("duplicate name created")
	}
	if code := runMCPTokenCommand([]string{"mcp-token-create", "--name", "nothing"}, &stdout, &stderr); code == 0 {
		t.Fatal("token without projects created")
	}
	stdout.Reset()
	stderr.Reset()
	if code := runMCPTokenCommand([]string{"mcp-token-create", "--name", "by-tag", "--tag", "#Coding"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "tag Coding: (no cache yet)") {
		t.Fatalf("create by tag exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runMCPTokenCommand([]string{"mcp-token-list"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "repo-a  read-only") || strings.Contains(stdout.String(), match) {
		t.Fatalf("list exit %d: %s", code, stdout.String())
	}
	stdout.Reset()
	if code := runMCPTokenCommand([]string{"mcp-token-remove", "repo-a"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Removed token") {
		t.Fatalf("remove exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	if code := runMCPTokenCommand([]string{"mcp-token-remove", "repo-a"}, &stdout, &stderr); code == 0 {
		t.Fatal("removing a missing token succeeded")
	}
	if code := runMCPTokenCommand([]string{"mcp-token-projects"}, &stdout, &stderr); code != 1 {
		t.Fatalf("projects without cache exit %d", code)
	}
	if code := runMCPTokenCommand([]string{"mcp-token-frobnicate"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
}

func TestMCPRequiresBearerToken(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	if response := h.rpc(mcpRequestOptions{}, "ping", nil); response.Code != http.StatusUnauthorized || !strings.HasPrefix(response.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("no token = %d %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	if response := h.rpc(mcpRequestOptions{token: token}, "ping", nil); response.Code != http.StatusOK {
		t.Fatalf("ping = %d %s", response.Code, response.Body)
	}
	if _, err := h.server.store.remove("laptop"); err != nil {
		t.Fatal(err)
	}
	if response := h.rpc(mcpRequestOptions{token: token}, "ping", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("removed token = %d", response.Code)
	}
	// Other credentials never count: a Basic Auth header or a cookie is not
	// a token.
	basic := mcpRequestOptions{headers: map[string]string{"Authorization": "Basic " + randomToken(), "Cookie": passkeyCookieName + "=" + randomToken()}}
	if response := h.rpc(basic, "ping", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("basic auth + cookie = %d, want 401", response.Code)
	}
}

func TestMCPFailedTokensAreRateLimited(t *testing.T) {
	h := newMCPTestServer(t)
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	h.server.limiter.now = func() time.Time { return now }
	token := h.createToken("laptop", false, mcpProjectA)
	for i := 0; i < authFailuresPerIP; i++ {
		if response := h.rpc(mcpRequestOptions{token: mcpTokenPrefix + randomToken()}, "ping", nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, response.Code)
		}
	}
	response := h.rpc(mcpRequestOptions{token: token}, "ping", nil)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("after %d failures = %d, want 429 with Retry-After", authFailuresPerIP, response.Code)
	}
	now = now.Add(authBlockDuration + time.Second)
	if response := h.rpc(mcpRequestOptions{token: token}, "ping", nil); response.Code != http.StatusOK {
		t.Fatalf("after the block = %d", response.Code)
	}
}

func TestMCPTransportChecks(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)
	ok := mcpRequestOptions{token: token}

	if response := h.rpc(mcpRequestOptions{token: token, method: http.MethodGet}, "ping", nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, method: http.MethodDelete}, "ping", nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, headers: map[string]string{"X-Forwarded-Proto": "http"}}, "ping", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("plain http = %d, want 400 rather than a redirect", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, headers: map[string]string{"Origin": "https://evil.example"}}, "ping", nil); response.Code != http.StatusForbidden {
		t.Fatalf("foreign origin = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, headers: map[string]string{"Origin": "https://things.example.test"}}, "ping", nil); response.Code != http.StatusOK {
		t.Fatalf("own origin = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, headers: map[string]string{"Content-Type": "text/plain"}}, "ping", nil); response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, raw: strings.Repeat(" ", maxRequestBodyBytes+1) + "{}"}, "", nil); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, raw: `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`}, "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("batch = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, raw: `{"jsonrpc":"2.0","method":"notifications/initialized"}`}, "", nil); response.Code != http.StatusAccepted {
		t.Fatalf("notification = %d", response.Code)
	}
	if response := h.rpc(mcpRequestOptions{token: token, raw: `not json`}, "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("parse error = %d", response.Code)
	}
	if response := h.rpc(ok, "resources/list", nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown method = %d", response.Code)
	}
	if response := h.rpc(ok, "ping", nil); response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	// Other paths pass through to the app.
	request := httptest.NewRequest(http.MethodGet, "https://things.example.test/api/sync", nil)
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("/api/sync through the MCP route = %d", recorder.Code)
	}
}

func TestMCPInitializeAndToolList(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)
	readOnly := h.createToken("viewer", true, mcpProjectA)

	response := h.rpc(mcpRequestOptions{token: token}, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}})
	var initialized struct {
		Result struct {
			ProtocolVersion string         `json:"protocolVersion"`
			ServerInfo      map[string]any `json:"serverInfo"`
			Capabilities    map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &initialized)
	if response.Code != http.StatusOK || initialized.Result.ProtocolVersion != "2025-03-26" || initialized.Result.Capabilities["tools"] == nil {
		t.Fatalf("initialize = %d %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), h.dir) {
		t.Fatal("initialize leaks server paths")
	}
	response = h.rpc(mcpRequestOptions{token: token}, "initialize", map[string]any{"protocolVersion": "1999-01-01"})
	_ = json.Unmarshal(response.Body.Bytes(), &initialized)
	if initialized.Result.ProtocolVersion != mcpProtocolVersion {
		t.Fatalf("unknown client version answered with %q", initialized.Result.ProtocolVersion)
	}

	names := func(token string) []string {
		response := h.rpc(mcpRequestOptions{token: token}, "tools/list", nil)
		var list struct {
			Result struct {
				Tools []struct {
					Name        string         `json:"name"`
					InputSchema map[string]any `json:"inputSchema"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil || response.Code != http.StatusOK {
			t.Fatalf("tools/list = %d %s", response.Code, response.Body)
		}
		result := []string{}
		for _, tool := range list.Result.Tools {
			if tool.InputSchema["type"] != "object" {
				t.Fatalf("tool %s without object schema", tool.Name)
			}
			result = append(result, tool.Name)
		}
		return result
	}
	if got := strings.Join(names(token), ","); got != "list_areas,list_tags,list_projects,list_todos,create_project,update_project,complete_todo,create_todo,update_todo,reorder_todo,add_checklist_items,update_checklist_item,remove_checklist_item,reorder_checklist" {
		t.Fatalf("tools = %s", got)
	}
	if got := strings.Join(names(readOnly), ","); got != "list_areas,list_tags,list_projects,list_todos" {
		t.Fatalf("read-only tools = %s", got)
	}
}

func TestMCPListsOnlyAllowedProjects(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	status, result, _ := h.tool(token, "list_projects", map[string]any{})
	projects, _ := result["projects"].([]any)
	if status != http.StatusOK || len(projects) != 1 || projects[0].(map[string]any)["uuid"] != mcpProjectA || projects[0].(map[string]any)["title"] != "Alpha" {
		t.Fatalf("list_projects = %d %v", status, result)
	}
	if result["syncedAt"] == "" {
		t.Fatal("list_projects without syncedAt")
	}

	status, result, _ = h.tool(token, "list_todos", map[string]any{"project": mcpProjectA})
	todos, _ := result["todos"].([]any)
	if status != http.StatusOK || len(todos) != 2 {
		t.Fatalf("list_todos(A) = %d %v", status, result)
	}
	first := todos[0].(map[string]any)
	second := todos[1].(map[string]any)
	if first["uuid"] != mcpTaskA1 || first["heading"] != nil || first["deadline"] != "2026-11-01" || first["when"] != "anytime" || first["status"] != "open" || first["position"] != float64(1) {
		t.Fatalf("first to-do = %v", first)
	}
	if checklist, _ := first["checklist"].([]any); len(checklist) != 2 || checklist[0].(map[string]any)["title"] != "Unit" || checklist[0].(map[string]any)["done"] != true || checklist[0].(map[string]any)["uuid"] != mcpItemA {
		t.Fatalf("checklist = %v", first["checklist"])
	}
	if heading, _ := second["heading"].(map[string]any); second["uuid"] != mcpTaskA2 || heading["title"] != "Phase 2" || heading["uuid"] != mcpHeadingA || second["when"] != "2026-11-03" || second["position"] != float64(2) {
		t.Fatalf("second to-do = %v", second)
	}
	if headings, _ := result["headings"].([]any); len(headings) != 1 || headings[0].(map[string]any)["uuid"] != mcpHeadingA {
		t.Fatalf("headings = %v", result["headings"])
	}
	if result["project"].(map[string]any)["title"] != "Alpha" {
		t.Fatalf("project = %v", result["project"])
	}

	status, result, _ = h.tool(token, "list_todos", map[string]any{"project": mcpProjectA, "include_completed": true})
	if todos, _ := result["todos"].([]any); status != http.StatusOK || len(todos) != 3 || todos[0].(map[string]any)["uuid"] != mcpTaskA3 || todos[0].(map[string]any)["status"] != "completed" {
		t.Fatalf("list_todos(A, include_completed) = %d %v", status, result)
	}

	if status, _, message := h.tool(token, "list_todos", map[string]any{"project": mcpProjectB}); status != http.StatusForbidden || message == "" {
		t.Fatalf("list_todos(B) = %d %q, want 403", status, message)
	}
	if status, _, _ := h.tool(token, "list_todos", map[string]any{"project": "--help"}); status != http.StatusBadRequest {
		t.Fatalf("list_todos(--help) = %d, want 400", status)
	}
	if status, _, _ := h.tool(token, "list_todos", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("list_todos() = %d, want 400", status)
	}
	if status, _, _ := h.tool(token, "delete_todo", map[string]any{"uuid": mcpTaskA1}); status != http.StatusNotFound {
		t.Fatalf("unknown tool = %d, want 404", status)
	}
}

// TestMCPTagScopesFollowTheModel pins that a tag token sees the projects
// carrying the tag in the current model, case-insensitively, and nothing
// else; fixed UUIDs add to it.
func TestMCPTagScopesFollowTheModel(t *testing.T) {
	h := newMCPTestServer(t)
	byTag, err := h.server.store.create("coding", nil, []string{"#coding"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	status, result, _ := h.tool(byTag, "list_projects", map[string]any{})
	projects, _ := result["projects"].([]any)
	// Beta carries the tag; Gamma is in the area Work, which carries it.
	if status != http.StatusOK || len(projects) != 2 || projects[0].(map[string]any)["uuid"] != mcpProjectB || projects[1].(map[string]any)["uuid"] != mcpProjectC {
		t.Fatalf("tag token list_projects = %d %v", status, result)
	}
	if tags, _ := projects[0].(map[string]any)["tags"].([]any); len(tags) != 2 || tags[0] != "Coding" {
		t.Fatalf("project tags = %v", projects[0])
	}
	if area, _ := projects[1].(map[string]any)["area"].(map[string]any); area["uuid"] != mcpAreaWork || area["title"] != "Work" {
		t.Fatalf("project area = %v", projects[1])
	}
	if status, _, _ := h.tool(byTag, "list_todos", map[string]any{"project": mcpProjectB}); status != http.StatusOK {
		t.Fatalf("tag token list_todos(B) = %d", status)
	}
	if status, _, _ := h.tool(byTag, "list_todos", map[string]any{"project": mcpProjectA}); status != http.StatusForbidden {
		t.Fatalf("tag token list_todos(A) = %d, want 403", status)
	}
	if status, _, _ := h.tool(byTag, "complete_todo", map[string]any{"uuid": mcpTaskA1}); status != http.StatusForbidden {
		t.Fatalf("tag token complete_todo(A1) = %d, want 403", status)
	}
	if status, result, _ := h.tool(byTag, "complete_todo", map[string]any{"uuid": mcpTaskB1}); status != http.StatusOK || result["changed"] != true {
		t.Fatalf("tag token complete_todo(B1) = %d %v", status, result)
	}
	waitIdle(t, models)

	both, err := h.server.store.create("both", []string{mcpProjectA}, []string{"Home"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	status, result, _ = h.tool(both, "list_projects", map[string]any{})
	if projects, _ := result["projects"].([]any); status != http.StatusOK || len(projects) != 2 {
		t.Fatalf("uuid+tag token list_projects = %d %v", status, result)
	}
	if status, result, _ := h.tool(both, "list_areas", map[string]any{}); status != http.StatusOK || len(result["areas"].([]any)) != 0 {
		t.Fatalf("token without a tagged area list_areas = %d %v", status, result)
	}
	other, _ := h.server.store.create("other", nil, []string{"Garden"}, false, time.Now())
	status, result, _ = h.tool(other, "list_projects", map[string]any{})
	if projects, _ := result["projects"].([]any); status != http.StatusOK || len(projects) != 0 {
		t.Fatalf("unmatched tag token list_projects = %d %v", status, result)
	}
}

func TestMCPCompleteIsBoundToTheProject(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	for name, uuid := range map[string]string{"foreign": mcpTaskB1, "unknown": "TaskUnknownnnnnnnnnnnnn", "project": mcpProjectA} {
		status, _, message := h.tool(token, "complete_todo", map[string]any{"uuid": uuid})
		if status != http.StatusForbidden || message != "The to-do is not in a project this token may access." {
			t.Fatalf("complete_todo(%s) = %d %q, want the same 403", name, status, message)
		}
	}
	if status, _, _ := h.tool(token, "complete_todo", map[string]any{"uuid": "--help"}); status != http.StatusBadRequest {
		t.Fatalf("complete_todo(--help) = %d", status)
	}
	if strings.Contains(h.calls(), "complete|") {
		t.Fatalf("CLI called for refused completes: %s", h.calls())
	}

	status, result, message := h.tool(token, "complete_todo", map[string]any{"uuid": mcpTaskA2})
	if status != http.StatusOK || result["changed"] != true || result["status"] != "completed" {
		t.Fatalf("complete_todo(A2) = %d %v %q", status, result, message)
	}
	if !strings.Contains(h.calls(), "complete|"+mcpTaskA2+"|") {
		t.Fatalf("CLI calls: %s", h.calls())
	}
	waitIdle(t, models)
	// The web app sees the to-do ticked (until it is logged), and the MCP
	// list hides it by default.
	status, result, _ = h.tool(token, "list_todos", map[string]any{"project": mcpProjectA})
	if todos, _ := result["todos"].([]any); status != http.StatusOK || len(todos) != 1 || todos[0].(map[string]any)["uuid"] != mcpTaskA1 {
		t.Fatalf("list_todos after complete = %v", result)
	}
	status, result, _ = h.tool(token, "complete_todo", map[string]any{"uuid": mcpTaskA2})
	if status != http.StatusOK || result["changed"] != false || strings.Count(h.calls(), "complete|") != 1 {
		t.Fatalf("second complete_todo = %d %v, calls %s", status, result, h.calls())
	}
}

func TestMCPCompleteRefusedForReadOnlyTokenAndInstance(t *testing.T) {
	h := newMCPTestServer(t)
	viewer := h.createToken("viewer", true, mcpProjectA)
	if status, _, message := h.tool(viewer, "complete_todo", map[string]any{"uuid": mcpTaskA1}); status != http.StatusForbidden || message != "This token is read-only." {
		t.Fatalf("read-only token = %d %q", status, message)
	}
	if status, result, _ := h.tool(viewer, "list_todos", map[string]any{"project": mcpProjectA}); status != http.StatusOK || len(result["todos"].([]any)) != 2 {
		t.Fatalf("read-only token list_todos = %d %v", status, result)
	}

	token := h.createToken("laptop", false, mcpProjectA)
	t.Setenv("APP_READ_ONLY", "1")
	if status, _, message := h.tool(token, "complete_todo", map[string]any{"uuid": mcpTaskA1}); status != http.StatusForbidden || !strings.Contains(message, "APP_READ_ONLY") {
		t.Fatalf("read-only instance = %d %q", status, message)
	}
	if status, _, _ := h.tool(token, "list_projects", map[string]any{}); status != http.StatusOK {
		t.Fatalf("read-only instance list_projects = %d", status)
	}
	if strings.Contains(h.calls(), "complete|") {
		t.Fatalf("CLI called: %s", h.calls())
	}
}

// TestMCPErrorsKeep401ForTokensOnly pins that a missing Things account on
// the server is not reported as 401, which clients read as a bad token.
func TestMCPErrorsKeep401ForTokensOnly(t *testing.T) {
	server := newMCPServer(t.TempDir(), newAuthLimiter())
	recorder := httptest.NewRecorder()
	server.writeRPCError(recorder, json.RawMessage("7"), statusError{Status: http.StatusUnauthorized, Message: "Sign in first."})
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"status":503`) || !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("no account = %d %s", recorder.Code, recorder.Body)
	}
	recorder = httptest.NewRecorder()
	server.writeRPCError(recorder, json.RawMessage("8"), statusError{Status: http.StatusFailedDependency, Message: "things-cli not found."})
	if recorder.Code != http.StatusFailedDependency {
		t.Fatalf("no CLI = %d", recorder.Code)
	}
}

func TestMCPWriteLimitPerToken(t *testing.T) {
	server := newMCPServer(t.TempDir(), newAuthLimiter())
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	for i := 0; i < mcpWritesPerHour; i++ {
		if !server.allowWrite("laptop") {
			t.Fatalf("write %d refused", i)
		}
	}
	if server.allowWrite("laptop") {
		t.Fatalf("write %d allowed", mcpWritesPerHour+1)
	}
	if !server.allowWrite("other") {
		t.Fatal("another token is limited too")
	}
	now = now.Add(mcpWriteWindow + time.Second)
	if !server.allowWrite("laptop") {
		t.Fatal("limit did not expire")
	}
}

// TestMCPRouteBypassesAuthMiddlewares pins that /mcp reaches the MCP server
// in both auth modes without a session, password or Origin, and that the
// rest of the app keeps its protection.
func TestMCPRouteBypassesAuthMiddlewares(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	post := func(handler http.Handler, path, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "https://things.example.test"+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	t.Setenv("APP_AUTH", "basic")
	t.Setenv("APP_PASSWORD", "correct horse battery staple")
	basic := appHandler(mux)
	if response := post(basic, "/mcp", token); response.Code != http.StatusOK {
		t.Fatalf("basic mode /mcp with token = %d %s", response.Code, response.Body)
	}
	if response := post(basic, "/mcp", ""); response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != `Bearer realm="NubeRerum"` {
		t.Fatalf("basic mode /mcp without token = %d %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	if response := post(basic, "/api/complete", token); response.Code != http.StatusUnauthorized || !strings.HasPrefix(response.Header().Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("basic mode /api/complete with an MCP token = %d, want Basic Auth challenge", response.Code)
	}

	t.Setenv("APP_AUTH", "passkey")
	t.Setenv("APP_PASSKEY_ORIGIN", testPasskeyOrigin)
	passkey := appHandler(mux)
	if response := post(passkey, "/mcp", token); response.Code != http.StatusOK {
		t.Fatalf("passkey mode /mcp with token = %d %s", response.Code, response.Body)
	}
	if response := post(passkey, "/mcp", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("passkey mode /mcp without token = %d", response.Code)
	}
	if response := post(passkey, "/api/complete", token); response.Code != http.StatusForbidden {
		t.Fatalf("passkey mode /api/complete with an MCP token = %d, want the same-origin guard", response.Code)
	}
}

// batchOps returns the operations of the n-th things-cli batch the fake CLI
// received (0-based).
func (h *mcpTestServer) batchOps(n int) []map[string]any {
	h.t.Helper()
	batches := []string{}
	for _, line := range strings.Split(h.calls(), "\n") {
		if strings.HasPrefix(line, "batch ") {
			batches = append(batches, strings.TrimPrefix(line, "batch "))
		}
	}
	if n >= len(batches) {
		h.t.Fatalf("batch %d not found in calls:\n%s", n, h.calls())
	}
	var ops []map[string]any
	if err := json.Unmarshal([]byte(batches[n]), &ops); err != nil {
		h.t.Fatal(err)
	}
	return ops
}

func TestMCPListTags(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("viewer", true, mcpProjectA)
	status, result, _ := h.tool(token, "list_tags", map[string]any{})
	tags, _ := result["tags"].([]any)
	if status != http.StatusOK || len(tags) != 3 || tags[0].(map[string]any)["title"] != "#coding" || tags[1].(map[string]any)["title"] != "code" || tags[2].(map[string]any)["uuid"] != "TagHomeeeeeeeeeeeeeeeee" {
		t.Fatalf("list_tags = %d %v", status, result)
	}
}

// TestMCPTagsMatchHashTitles pins that a stored tag whose title starts with
// "#" is found by "coding" and "#Coding" alike, and reported as stored.
func TestMCPTagsMatchHashTitles(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)
	for i, request := range []string{"coding", "#Coding", " #CODING "} {
		status, result, message := h.tool(token, "update_todo", map[string]any{"uuid": mcpTaskA1, "tags": []string{request}})
		if status != http.StatusOK {
			t.Fatalf("update_todo(tags %q) = %d %q", request, status, message)
		}
		if tags, _ := result["todo"].(map[string]any)["tags"].([]any); len(tags) != 1 || tags[0] != "#coding" {
			t.Fatalf("tags after %q = %v, want the stored title", request, result["todo"].(map[string]any)["tags"])
		}
		if ops := h.batchOps(i); len(ops) != 1 || ops[0]["cmd"] != "tags" || ops[0]["tags"].([]any)[0] != mcpTagHash {
			t.Fatalf("tag ops for %q = %v", request, ops)
		}
	}
	if status, _, message := h.tool(token, "update_todo", map[string]any{"uuid": mcpTaskA1, "tags": []string{"#nope"}}); status != http.StatusBadRequest || !strings.Contains(message, `Unknown tag "nope"`) {
		t.Fatalf("unknown hash tag = %d %q", status, message)
	}
}

func TestMCPCreateTodoInAllowedProject(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	if status, _, _ := h.tool(token, "create_todo", map[string]any{"project": mcpProjectB, "title": "Nope"}); status != http.StatusForbidden {
		t.Fatalf("create_todo(B) = %d, want 403", status)
	}
	for name, args := range map[string]map[string]any{
		"no title":        {"project": mcpProjectA, "title": " "},
		"option title":    {"project": mcpProjectA, "title": "--help"},
		"bad when":        {"project": mcpProjectA, "title": "x", "when": "tomorrow"},
		"bad deadline":    {"project": mcpProjectA, "title": "x", "deadline": "soon"},
		"foreign heading": {"project": mcpProjectA, "title": "x", "heading": "HeadingZzzzzzzzzzzzzzzz"},
		"unknown tag":     {"project": mcpProjectA, "title": "x", "tags": []string{"Nope"}},
		"bad position":    {"project": mcpProjectA, "title": "x", "position": "middle"},
	} {
		if status, _, message := h.tool(token, "create_todo", args); status != http.StatusBadRequest {
			t.Fatalf("create_todo(%s) = %d %q, want 400", name, status, message)
		}
	}
	if strings.Contains(h.calls(), "batch") {
		t.Fatalf("CLI called for refused creates: %s", h.calls())
	}

	status, result, message := h.tool(token, "create_todo", map[string]any{
		"project": mcpProjectA, "title": "Ship it", "note": "Details", "heading": mcpHeadingA,
		"tags": []string{"#Code", "Fresh"}, "create_missing_tags": true, "when": "2026-12-01", "deadline": "2026-12-24",
		"checklist": []string{"Build", "Test"},
	})
	if status != http.StatusOK || result["created"] != true {
		t.Fatalf("create_todo = %d %v %q", status, result, message)
	}
	todo := result["todo"].(map[string]any)
	if todo["uuid"] != mcpNewUUID1 || todo["title"] != "Ship it" || todo["when"] != "2026-12-01" || todo["deadline"] != "2026-12-24" {
		t.Fatalf("created to-do = %v", todo)
	}
	if heading, _ := todo["heading"].(map[string]any); heading["uuid"] != mcpHeadingA {
		t.Fatalf("heading = %v", todo["heading"])
	}
	if tags, _ := todo["tags"].([]any); len(tags) != 2 || tags[0] != "code" || tags[1] != "Fresh" {
		t.Fatalf("tags = %v", todo["tags"])
	}
	if checklist, _ := todo["checklist"].([]any); len(checklist) != 2 || checklist[1].(map[string]any)["title"] != "Test" || checklist[0].(map[string]any)["uuid"] != mcpNewUUID2 || checklist[1].(map[string]any)["uuid"] != mcpNewUUID3 {
		t.Fatalf("checklist = %v", todo["checklist"])
	}
	if !strings.Contains(h.calls(), "create-tag|Fresh|") {
		t.Fatalf("missing tag not created: %s", h.calls())
	}
	create := h.batchOps(0)
	if len(create) != 1 || create[0]["cmd"] != "create" || create[0]["project"] != mcpProjectA || create[0]["heading"] != mcpHeadingA || create[0]["title"] != "Ship it" || create[0]["note"] != "Details" || create[0]["extra"].(map[string]any)["scheduled"] != "2026-12-01" {
		t.Fatalf("create ops = %v", create)
	}
	// One write per item and commit: tags, deadline and reorder for the new
	// to-do go into successive batches; the checklist items ride along.
	extras := h.batchOps(1)
	cmds := []string{}
	for _, op := range extras {
		cmds = append(cmds, asString(op, "cmd"))
	}
	if strings.Join(cmds, ",") != "tags,checklist-add,checklist-add" {
		t.Fatalf("extra ops = %v", extras)
	}
	if ids, _ := extras[0]["tags"].([]any); len(ids) != 2 || ids[0] != mcpTagCode || ids[1] != mcpNewUUID3 {
		t.Fatalf("tag ids = %v", extras[0]["tags"])
	}
	if extras[1]["task"] != mcpNewUUID1 || extras[1]["title"] != "Build" {
		t.Fatalf("extra ops = %v", extras)
	}
	if deadline := h.batchOps(2); len(deadline) != 1 || deadline[0]["deadlineDate"] != "2026-12-24" {
		t.Fatalf("deadline ops = %v", deadline)
	}
	// Bottom of the project: after Alpha's highest index (2).
	if reorder := h.batchOps(3); len(reorder) != 1 || reorder[0]["uuid"] != mcpNewUUID1 || reorder[0]["index"] != float64(2+reorderStep) {
		t.Fatalf("reorder op = %v", reorder)
	}

	// Default: anytime, bottom; "top" goes above the first to-do.
	status, _, _ = h.tool(token, "create_todo", map[string]any{"project": mcpProjectA, "title": "Top", "position": "top"})
	if status != http.StatusOK {
		t.Fatalf("create_todo(top) = %d", status)
	}
	if create := h.batchOps(4); create[0]["when"] != "anytime" {
		t.Fatalf("default when = %v", create[0]["when"])
	}
	if extras := h.batchOps(5); len(extras) != 1 || extras[0]["cmd"] != "reorder" || extras[0]["index"].(float64) >= 0 {
		t.Fatalf("top reorder = %v", extras)
	}
}

func TestMCPUpdateTodo(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	if status, _, _ := h.tool(token, "update_todo", map[string]any{"uuid": mcpTaskB1, "title": "Nope"}); status != http.StatusForbidden {
		t.Fatalf("update_todo(B1) = %d, want 403", status)
	}
	if status, _, _ := h.tool(token, "update_todo", map[string]any{"uuid": mcpTaskA1}); status != http.StatusBadRequest {
		t.Fatalf("update_todo without fields = %d, want 400", status)
	}
	if status, _, _ := h.tool(token, "update_todo", map[string]any{"uuid": mcpTaskA1, "note": "--oops"}); status != http.StatusBadRequest {
		t.Fatalf("update_todo(option note) = %d, want 400", status)
	}

	status, result, message := h.tool(token, "update_todo", map[string]any{
		"uuid": mcpTaskA1, "title": "Write more tests", "note": "Now\n", "tags": []string{"home"}, "when": "today", "deadline": "", "heading": mcpHeadingA,
	})
	if status != http.StatusOK || result["updated"] != true {
		t.Fatalf("update_todo = %d %v %q", status, result, message)
	}
	todo := result["todo"].(map[string]any)
	if todo["title"] != "Write more tests" || todo["note"] != "Now" || todo["deadline"] != "" || todo["when"] != "today" {
		t.Fatalf("updated to-do = %v", todo)
	}
	if tags, _ := todo["tags"].([]any); len(tags) != 1 || tags[0] != "Home" {
		t.Fatalf("tags = %v", todo["tags"])
	}
	if heading, _ := todo["heading"].(map[string]any); heading["uuid"] != mcpHeadingA || heading["title"] != "Phase 2" {
		t.Fatalf("heading = %v", todo["heading"])
	}
	if !strings.Contains(h.calls(), "edit|"+mcpTaskA1+"|--title|Write more tests|--note|Now|") {
		t.Fatalf("edit call missing: %s", h.calls())
	}
	// One write per item and commit: four batches for the four fields.
	cmds := []string{}
	for i := 0; i < 4; i++ {
		ops := h.batchOps(i)
		if len(ops) != 1 {
			t.Fatalf("batch %d = %v, want one op", i, ops)
		}
		cmds = append(cmds, asString(ops[0], "cmd"))
	}
	if strings.Join(cmds, ",") != "tags,schedule,deadline,move" || h.batchOps(1)[0]["when"] != "today" || h.batchOps(2)[0]["deadlineDate"] != "" || h.batchOps(3)[0]["heading"] != mcpHeadingA {
		t.Fatalf("update ops = %v", cmds)
	}

	// Leaving a heading moves the to-do to the project itself.
	if status, _, _ := h.tool(token, "update_todo", map[string]any{"uuid": mcpTaskA2, "heading": ""}); status != http.StatusOK {
		t.Fatalf("update_todo(remove heading) = %d", status)
	}
	if ops := h.batchOps(4); len(ops) != 1 || ops[0]["cmd"] != "move" || ops[0]["project"] != mcpProjectA {
		t.Fatalf("remove heading ops = %v", ops)
	}
}

func TestMCPReorderTodo(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	if status, _, _ := h.tool(token, "reorder_todo", map[string]any{"uuid": mcpTaskA1, "after": mcpTaskB1}); status != http.StatusForbidden {
		t.Fatalf("reorder next to a foreign to-do = %d, want 403", status)
	}
	if status, _, _ := h.tool(token, "reorder_todo", map[string]any{"uuid": mcpTaskA1}); status != http.StatusBadRequest {
		t.Fatalf("reorder without target = %d, want 400", status)
	}
	if status, _, _ := h.tool(token, "reorder_todo", map[string]any{"uuid": mcpTaskA1, "after": mcpTaskA2, "position": "top"}); status != http.StatusBadRequest {
		t.Fatalf("reorder with two targets = %d, want 400", status)
	}
	// A3 (index 0) is in the same section as A1 (index 2): A1 before A3.
	status, result, message := h.tool(token, "reorder_todo", map[string]any{"uuid": mcpTaskA1, "before": mcpTaskA3})
	if status != http.StatusOK || result["reordered"] != true {
		t.Fatalf("reorder_todo = %d %v %q", status, result, message)
	}
	ops := h.batchOps(0)
	if len(ops) != 1 || ops[0]["cmd"] != "reorder" || ops[0]["uuid"] != mcpTaskA1 || ops[0]["index"] != float64(-reorderStep) {
		t.Fatalf("reorder ops = %v", ops)
	}
	// After A2, which is under a heading: A1 joins the heading.
	status, result, _ = h.tool(token, "reorder_todo", map[string]any{"uuid": mcpTaskA1, "after": mcpTaskA2})
	if status != http.StatusOK {
		t.Fatalf("reorder_todo(after A2) = %d %v", status, result)
	}
	move, reorder := h.batchOps(1), h.batchOps(2)
	if len(move) != 1 || move[0]["cmd"] != "move" || move[0]["heading"] != mcpHeadingA || len(reorder) != 1 || reorder[0]["cmd"] != "reorder" || reorder[0]["index"] != float64(1+reorderStep) {
		t.Fatalf("reorder into heading ops = %v %v", move, reorder)
	}
	if heading, _ := result["todo"].(map[string]any)["heading"].(map[string]any); heading["uuid"] != mcpHeadingA {
		t.Fatalf("to-do after reorder = %v", result["todo"])
	}
}

func TestMCPChecklistTools(t *testing.T) {
	h := newMCPTestServer(t)
	token := h.createToken("laptop", false, mcpProjectA)

	if status, _, _ := h.tool(token, "add_checklist_items", map[string]any{"todo": mcpTaskB1, "titles": []string{"x"}}); status != http.StatusForbidden {
		t.Fatalf("checklist on a foreign to-do = %d, want 403", status)
	}
	if status, _, _ := h.tool(token, "update_checklist_item", map[string]any{"todo": mcpTaskA1, "item": "ItemZzzzzzzzzzzzzzzzzzz", "done": true}); status != http.StatusBadRequest {
		t.Fatalf("unknown item = %d, want 400", status)
	}
	if status, _, _ := h.tool(token, "add_checklist_items", map[string]any{"todo": mcpTaskA1, "titles": []string{"--x"}}); status != http.StatusBadRequest {
		t.Fatalf("option item = %d, want 400", status)
	}

	status, result, message := h.tool(token, "add_checklist_items", map[string]any{"todo": mcpTaskA1, "titles": []string{"Docs", "Release"}})
	if status != http.StatusOK {
		t.Fatalf("add_checklist_items = %d %q", status, message)
	}
	checklist, _ := result["checklist"].([]any)
	if len(checklist) != 4 || checklist[2].(map[string]any)["title"] != "Docs" || checklist[2].(map[string]any)["uuid"] != mcpNewUUID1 || checklist[3].(map[string]any)["uuid"] != mcpNewUUID2 {
		t.Fatalf("checklist after add = %v", checklist)
	}
	ops := h.batchOps(0)
	if len(ops) != 2 || ops[0]["cmd"] != "checklist-add" || ops[0]["task"] != mcpTaskA1 || ops[0]["index"] != float64(1+reorderStep) || ops[1]["index"] != float64(1+2*reorderStep) {
		t.Fatalf("add ops = %v", ops)
	}

	status, result, _ = h.tool(token, "update_checklist_item", map[string]any{"todo": mcpTaskA1, "item": mcpItemB, "title": "End to end", "done": true})
	checklist, _ = result["checklist"].([]any)
	if status != http.StatusOK || checklist[1].(map[string]any)["title"] != "End to end" || checklist[1].(map[string]any)["done"] != true {
		t.Fatalf("update_checklist_item = %d %v", status, result)
	}
	if edit, done := h.batchOps(1), h.batchOps(2); len(edit) != 1 || edit[0]["cmd"] != "checklist-edit" || len(done) != 1 || done[0]["cmd"] != "checklist-complete" || done[0]["done"] != true {
		t.Fatalf("update ops = %v %v", edit, done)
	}

	status, result, _ = h.tool(token, "remove_checklist_item", map[string]any{"todo": mcpTaskA1, "item": mcpItemA})
	if checklist, _ := result["checklist"].([]any); status != http.StatusOK || len(checklist) != 3 || checklist[0].(map[string]any)["uuid"] != mcpItemB || checklist[2].(map[string]any)["uuid"] != mcpNewUUID2 {
		t.Fatalf("remove_checklist_item = %d %v", status, result)
	}
	if ops := h.batchOps(3); len(ops) != 1 || ops[0]["cmd"] != "checklist-delete" || ops[0]["uuid"] != mcpItemA {
		t.Fatalf("remove ops = %v", ops)
	}

	if status, _, _ := h.tool(token, "reorder_checklist", map[string]any{"todo": mcpTaskA1, "order": []string{"ItemZzzzzzzzzzzzzzzzzzz"}}); status != http.StatusBadRequest {
		t.Fatalf("reorder with an unknown item = %d, want 400", status)
	}
	// Remaining: B, Docs, Release. Reverse them.
	status, result, _ = h.tool(token, "reorder_checklist", map[string]any{"todo": mcpTaskA1, "order": []string{mcpNewUUID2, mcpNewUUID1, mcpItemB}})
	if checklist, _ := result["checklist"].([]any); status != http.StatusOK || len(checklist) != 3 || checklist[0].(map[string]any)["uuid"] != mcpNewUUID2 || checklist[2].(map[string]any)["uuid"] != mcpItemB {
		t.Fatalf("reorder_checklist = %d %v", status, result)
	}
	if ops := h.batchOps(4); len(ops) != 3 || ops[0]["cmd"] != "checklist-reorder" || ops[0]["uuid"] != mcpNewUUID2 || ops[0]["index"] != float64(0) || ops[2]["uuid"] != mcpItemB || ops[2]["index"] != float64(2*reorderStep) {
		t.Fatalf("reorder ops = %v", ops)
	}
	waitIdle(t, models)
	status, result, _ = h.tool(token, "list_todos", map[string]any{"project": mcpProjectA})
	first := result["todos"].([]any)[0].(map[string]any)
	if checklist, _ := first["checklist"].([]any); status != http.StatusOK || len(checklist) != 3 || checklist[0].(map[string]any)["title"] != "Release" {
		t.Fatalf("checklist after sync = %v", first["checklist"])
	}
}

// TestMCPTaggedAreaAllowsProjects pins that an area carrying the token's
// tag grants its projects and creating projects in it, and nothing in
// other areas.
func TestMCPTaggedAreaAllowsProjects(t *testing.T) {
	h := newMCPTestServer(t)
	byTag, err := h.server.store.create("coding", nil, []string{"coding"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	status, result, _ := h.tool(byTag, "list_areas", map[string]any{})
	areas, _ := result["areas"].([]any)
	if status != http.StatusOK || len(areas) != 1 || areas[0].(map[string]any)["uuid"] != mcpAreaWork || areas[0].(map[string]any)["title"] != "Work" {
		t.Fatalf("list_areas = %d %v", status, result)
	}
	if status, _, _ := h.tool(byTag, "list_todos", map[string]any{"project": mcpProjectC}); status != http.StatusOK {
		t.Fatalf("list_todos(project in tagged area) = %d", status)
	}
	if status, _, message := h.tool(byTag, "create_project", map[string]any{"area": mcpAreaHome, "title": "Nope"}); status != http.StatusForbidden {
		t.Fatalf("create_project in an untagged area = %d %q, want 403", status, message)
	}
	for name, args := range map[string]map[string]any{
		"no title":    {"area": mcpAreaWork, "title": ""},
		"bad when":    {"area": mcpAreaWork, "title": "x", "when": "today"},
		"bad area":    {"area": "--x", "title": "x"},
		"unknown tag": {"area": mcpAreaWork, "title": "x", "tags": []string{"Nope"}},
	} {
		if status, _, _ := h.tool(byTag, "create_project", args); status != http.StatusBadRequest {
			t.Fatalf("create_project(%s) = %d, want 400", name, status)
		}
	}
	if strings.Contains(h.calls(), "batch") {
		t.Fatalf("CLI called for refused creates: %s", h.calls())
	}

	status, result, message := h.tool(byTag, "create_project", map[string]any{"area": mcpAreaWork, "title": "New Service", "note": "Spec", "tags": []string{"#Code"}, "when": "someday"})
	if status != http.StatusOK || result["created"] != true {
		t.Fatalf("create_project = %d %v %q", status, result, message)
	}
	project := result["project"].(map[string]any)
	if project["uuid"] != mcpNewUUID1 || project["title"] != "New Service" || project["when"] != "someday" || project["area"].(map[string]any)["title"] != "Work" {
		t.Fatalf("created project = %v", project)
	}
	if tags, _ := project["tags"].([]any); len(tags) != 1 || tags[0] != "code" {
		t.Fatalf("project tags = %v", project["tags"])
	}
	create := h.batchOps(0)
	if len(create) != 1 || create[0]["cmd"] != "create" || create[0]["type"] != "project" || create[0]["area"] != mcpAreaWork || create[0]["when"] != "someday" || create[0]["note"] != "Spec" {
		t.Fatalf("create ops = %v", create)
	}
	if tags := h.batchOps(1); len(tags) != 1 || tags[0]["cmd"] != "tags" || tags[0]["uuid"] != mcpNewUUID1 || tags[0]["tags"].([]any)[0] != mcpTagCode {
		t.Fatalf("tag ops = %v", tags)
	}
	// The new project is in scope through its area, before any sync.
	status, result, _ = h.tool(byTag, "list_projects", map[string]any{})
	found := false
	for _, row := range result["projects"].([]any) {
		found = found || row.(map[string]any)["uuid"] == mcpNewUUID1
	}
	if status != http.StatusOK || !found {
		t.Fatalf("new project missing from list_projects: %v", result)
	}

	if status, _, _ := h.tool(byTag, "update_project", map[string]any{"uuid": mcpProjectA, "title": "Nope"}); status != http.StatusForbidden {
		t.Fatalf("update_project(A) = %d, want 403", status)
	}
	if status, _, _ := h.tool(byTag, "update_project", map[string]any{"uuid": mcpProjectC}); status != http.StatusBadRequest {
		t.Fatalf("update_project without fields = %d, want 400", status)
	}
	if status, result, _ := h.tool(byTag, "update_project", map[string]any{"uuid": mcpProjectC, "title": "Gamma 2", "note": "Hi"}); status != http.StatusOK || result["updated"] != true {
		t.Fatalf("update_project = %d %v", status, result)
	}
	if !strings.Contains(h.calls(), "edit|"+mcpProjectC+"|--title|Gamma 2|--note|Hi|") {
		t.Fatalf("edit call missing: %s", h.calls())
	}
}

func TestMCPWriteToolsRefusedForReadOnly(t *testing.T) {
	h := newMCPTestServer(t)
	viewer := h.createToken("viewer", true, mcpProjectA)
	for _, name := range []string{"create_project", "update_project", "create_todo", "update_todo", "reorder_todo", "add_checklist_items", "update_checklist_item", "remove_checklist_item", "reorder_checklist"} {
		if status, _, message := h.tool(viewer, name, map[string]any{"project": mcpProjectA, "uuid": mcpTaskA1, "todo": mcpTaskA1, "title": "x"}); status != http.StatusForbidden || message != "This token is read-only." {
			t.Fatalf("%s with a read-only token = %d %q", name, status, message)
		}
	}
	token := h.createToken("laptop", false, mcpProjectA)
	t.Setenv("APP_READ_ONLY", "1")
	if status, _, message := h.tool(token, "create_todo", map[string]any{"project": mcpProjectA, "title": "x"}); status != http.StatusForbidden || !strings.Contains(message, "APP_READ_ONLY") {
		t.Fatalf("create_todo on a read-only instance = %d %q", status, message)
	}
	if strings.Contains(h.calls(), "batch") {
		t.Fatalf("CLI called: %s", h.calls())
	}
}

func TestMCPBatchRoundsSplitWritesPerItem(t *testing.T) {
	ops := []map[string]any{
		{"cmd": "tags", "uuid": "X"},
		{"cmd": "checklist-add", "task": "X"},
		{"cmd": "deadline", "uuid": "X"},
		{"cmd": "reorder", "uuid": "Y"},
		{"cmd": "reorder", "uuid": "X"},
	}
	rounds := mcpBatchRounds(ops)
	got := []string{}
	for _, round := range rounds {
		names := []string{}
		for _, op := range round.ops {
			names = append(names, asString(op, "cmd"))
		}
		got = append(got, strings.Join(names, "+")+fmt.Sprint(round.indexes))
	}
	if want := "tags+checklist-add+reorder[0 1 3],deadline[2],reorder[4]"; strings.Join(got, ",") != want {
		t.Fatalf("rounds = %v, want %s", got, want)
	}
	err := mcpBatchFailure(statusError{Status: http.StatusBadGateway, Message: "Things Cloud write failed: boom."}, rounds, 1)
	var failure mcpError
	if !errors.As(err, &failure) || failure.status != http.StatusBadGateway || failure.message != "Things Cloud write failed: boom. Not written: deadline; already written: tags, checklist-add, reorder; skipped: reorder" {
		t.Fatalf("failure = %v", err)
	}
}
