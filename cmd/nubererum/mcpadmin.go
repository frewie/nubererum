package main

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// MCP token management in the web app, the same as the mcp-token-*
// commands: GET /api/mcp-tokens lists the tokens and what they cover, POST
// /api/mcp-token-create makes one and returns its value once, POST
// /api/mcp-token-remove deletes one. Signing in to the app (passkey session
// or Basic Auth, behind the same-origin guard) is the credential; an MCP
// token can never manage tokens. They are not in writePaths: they change
// the app's token file, not Things data.

func mcpTokenStoreForApp() *mcpTokenStore {
	return newMCPTokenStore(passkeyDataDir())
}

// mcpTokenView is a token as the web app shows it, without its hash.
func mcpTokenView(token mcpToken, model map[string]any) map[string]any {
	titles := map[string]string{}
	for _, project := range mcpModelProjects(model) {
		titles[asString(project, "uuid")] = asString(project, "title")
	}
	projects := []map[string]any{}
	for _, uuid := range token.Projects {
		_, open := titles[uuid]
		projects = append(projects, map[string]any{"uuid": uuid, "title": titles[uuid], "open": open})
	}
	covers := []map[string]any{}
	for uuid := range token.allowed(model) {
		if title, ok := titles[uuid]; ok {
			covers = append(covers, map[string]any{"uuid": uuid, "title": title})
		}
	}
	sort.Slice(covers, func(i, j int) bool {
		return strings.ToLower(asString(covers[i], "title")) < strings.ToLower(asString(covers[j], "title"))
	})
	areas := []map[string]any{}
	allowedAreas := token.allowedAreas(model)
	all, _ := model["areas"].([]map[string]any)
	for _, area := range all {
		if allowedAreas[asString(area, "uuid")] {
			areas = append(areas, map[string]any{"uuid": asString(area, "uuid"), "title": asString(area, "title")})
		}
	}
	view := map[string]any{
		"name":      token.Name,
		"readOnly":  token.ReadOnly,
		"tags":      append([]string{}, token.Tags...),
		"projects":  projects,
		"covers":    covers,
		"areas":     areas,
		"createdAt": token.CreatedAt.UTC().Format(time.RFC3339),
	}
	if !token.LastUsedAt.IsZero() {
		view["lastUsedAt"] = token.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return view
}

// mcpAdminModel is the current model for the coverage, or nil before the
// first sync (the token list still works then).
func mcpAdminModel() map[string]any {
	if result, err := models.cached(); err == nil {
		return result.model
	}
	return nil
}

// handleMCPTokens serves GET /api/mcp-tokens.
func handleMCPTokens(w http.ResponseWriter, _ *http.Request) {
	data, err := mcpTokenStoreForApp().load()
	if err != nil {
		logError("mcp tokens: reading the token file failed", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "The token file could not be read."})
		return
	}
	model := mcpAdminModel()
	tokens := append([]mcpToken(nil), data.Tokens...)
	sort.SliceStable(tokens, func(i, j int) bool { return tokens[i].CreatedAt.Before(tokens[j].CreatedAt) })
	views := []map[string]any{}
	for _, token := range tokens {
		views = append(views, mcpTokenView(token, model))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": views, "path": mcpPath})
}

// handleMCPTokenCreate serves POST /api/mcp-token-create
// {"name","projects","tags","readOnly"} and returns the token once.
func handleMCPTokenCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string   `json:"name"`
		Projects []string `json:"projects"`
		Tags     []string `json:"tags"`
		ReadOnly bool     `json:"readOnly"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(body.Projects) == 0 && len(body.Tags) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Choose at least one project or tag."})
		return
	}
	store := mcpTokenStoreForApp()
	value, err := store.create(name, body.Projects, body.Tags, body.ReadOnly, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	created, _ := store.find(value)
	slog.Info("mcp: token created in the web app", "token", created.Name, "readOnly", created.ReadOnly)
	writeJSON(w, http.StatusOK, map[string]any{"token": value, "created": mcpTokenView(created, mcpAdminModel()), "path": mcpPath})
}

// handleMCPTokenRemove serves POST /api/mcp-token-remove {"name"}.
func handleMCPTokenRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	removed, err := mcpTokenStoreForApp().remove(strings.TrimSpace(body.Name))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "No token with that name."})
		return
	}
	slog.Info("mcp: token removed in the web app", "token", removed.Name)
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed.Name})
}
