package main

import (
	"maps"
	"net/http"
	"slices"
	"strings"
)

// withTagChanged returns a patch that renames the tag uuid (title set) or
// removes it (title nil) in the tag list and on every to-do, project and
// area, whose tags (titles) follow their tagIDs.
func withTagChanged(uuid string, title *string) func(map[string]any) map[string]any {
	return func(model map[string]any) map[string]any {
		next := maps.Clone(model)
		if tags, ok := model["tags"].([]map[string]any); ok {
			copied := make([]map[string]any, 0, len(tags))
			for _, tag := range tags {
				if asString(tag, "uuid") == uuid {
					if title == nil {
						continue
					}
					tag = maps.Clone(tag)
					tag["title"] = *title
				}
				copied = append(copied, tag)
			}
			next["tags"] = copied
		}
		for _, key := range []string{"tasks", "projects", "areas"} {
			rows, ok := model[key].([]map[string]any)
			if !ok {
				continue
			}
			copied := make([]map[string]any, len(rows))
			for i, row := range rows {
				ids := asStringSlice(row["tagIDs"])
				if at := slices.Index(ids, uuid); at >= 0 {
					row = maps.Clone(row)
					titles := slices.Clone(asStringSlice(row["tags"]))
					switch {
					case title == nil:
						row["tagIDs"] = slices.Delete(slices.Clone(ids), at, at+1)
						if at < len(titles) {
							titles = slices.Delete(titles, at, at+1)
						}
					case at < len(titles):
						titles[at] = *title
					}
					row["tags"] = titles
				}
				copied[i] = row
			}
			next[key] = copied
		}
		return next
	}
}

// handleTagRename serves POST /api/tag-rename {"uuid","title"}.
func handleTagRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID  string `json:"uuid"`
		Title string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if !validThingsID(body.UUID) || title == "" || !validText(title, 200) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The name must not be empty or start with --."})
		return
	}
	edit := func([]string) func(map[string]any) map[string]any { return withTagChanged(body.UUID, &title) }
	if _, ok := writeBatch(w, []map[string]any{{"cmd": "tag-rename", "uuid": body.UUID, "title": title}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTagDelete serves POST /api/tag-delete {"uuid"}: Tag4 action 2, as
// Things deletes a tag. To-dos keep its id; the model drops unknown ids.
func handleTagDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	if !validThingsID(body.UUID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid tag."})
		return
	}
	edit := func([]string) func(map[string]any) map[string]any { return withTagChanged(body.UUID, nil) }
	if _, ok := writeBatch(w, []map[string]any{{"cmd": "tag-delete", "uuid": body.UUID}}, edit); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
