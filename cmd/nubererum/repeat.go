package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Things' repeat rule (rr) as read from the history on 2026-10-29:
// fu is the unit (16 day, 256 week, 8 month, 4 year), fa the amount, tp 0
// a fixed schedule and 1 "after completion", ed the end date (year 4001:
// never), of the details: wd a weekday (0 Sunday), dy a 0-based day of the
// month (-1 the last day), wdo with wd the nth weekday of the month (-1 the
// last).
var repeatUnits = map[int]string{16: "day", 256: "week", 8: "month", 4: "year"}

var weekdayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// repeatSummary describes how a to-do repeats, e.g. "Every week on Friday"
// or "Every 2 days after completion": its own rule for a template, the
// template's rule for an instance (RecurrenceIDs), "" otherwise.
func repeatSummary(task map[string]any, tasks map[string]map[string]any) string {
	rule, _ := task["RepeatRule"].(map[string]any)
	if rule == nil {
		for _, id := range asStringSlice(task["RecurrenceIDs"]) {
			if template := tasks[id]; template != nil {
				rule, _ = template["RepeatRule"].(map[string]any)
			}
			break
		}
	}
	if rule == nil {
		return ""
	}
	return describeRepeatRule(rule)
}

func describeRepeatRule(rule map[string]any) string {
	unit, ok := repeatUnits[int(asNumber(rule, "fu"))]
	if !ok {
		return "Repeating"
	}
	amount := int(asNumber(rule, "fa"))
	text := "Every " + unit
	if amount > 1 {
		text = fmt.Sprintf("Every %d %ss", amount, unit)
	}
	if int(asNumber(rule, "tp")) == 1 {
		text += " after completion"
	} else if detail := repeatDetail(unit, rule); detail != "" {
		text += " " + detail
	}
	if end := asNumber(rule, "ed"); end > 0 {
		if until := time.Unix(int64(end), 0).UTC(); until.Year() < 4001 && until.Year() > 1970 {
			text += " until " + until.Format("2006-01-02")
		}
	}
	return text
}

func repeatDetail(unit string, rule map[string]any) string {
	var details []map[string]any
	if list, ok := rule["of"].([]any); ok {
		for _, item := range list {
			if detail, ok := item.(map[string]any); ok {
				details = append(details, detail)
			}
		}
	}
	var parts []string
	for _, detail := range details {
		_, hasWeekday := detail["wd"]
		weekday := int(asNumber(detail, "wd"))
		switch {
		case unit == "week" && hasWeekday && weekday >= 0 && weekday < 7:
			parts = append(parts, weekdayNames[weekday])
		case unit == "month" && hasWeekday && weekday >= 0 && weekday < 7:
			if _, ok := detail["wdo"]; ok {
				parts = append(parts, "the "+ordinalWord(int(asNumber(detail, "wdo")))+" "+weekdayNames[weekday])
			}
		case unit == "month":
			if _, ok := detail["dy"]; ok {
				if day := int(asNumber(detail, "dy")); day == -1 {
					parts = append(parts, "the last day")
				} else if day >= 0 && day < 31 {
					parts = append(parts, "the "+ordinal(day+1))
				}
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "on " + strings.Join(parts, ", ")
}

func ordinalWord(n int) string {
	switch n {
	case -1:
		return "last"
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	case 4:
		return "fourth"
	case 5:
		return "fifth"
	}
	return ordinal(n)
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// handleRepeat serves POST /api/repeat {"uuid","type","unit","every",
// "month"?,"start"}: it makes an open to-do repeat. Like Things, a template
// carries the rule; the Things apps create its to-dos from the first day
// (start) on. The template copies the title, notes, project or area, heading
// and tags in the same write that moves the original, with its checklist, to
// the Trash. Restoring the original and trashing the template undoes it.
func handleRepeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID  string `json:"uuid"`
		Type  string `json:"type"`
		Unit  string `json:"unit"`
		Every int    `json:"every"`
		Month string `json:"month"`
		Start string `json:"start"`
	}
	if err := readJSON(r, &body); err != nil {
		errorResponse(w, err)
		return
	}
	units := map[string]bool{"day": true, "week": true, "month": true, "year": true}
	months := map[string]bool{"": true, "day": true, "last": true, "weekday": true}
	if !validThingsID(body.UUID) || (body.Type != "fixed" && body.Type != "after") || !units[body.Unit] || body.Every < 1 || body.Every > 99 ||
		!months[body.Month] || (body.Month != "" && body.Unit != "month") || !validDate(body.Start) || body.Start < time.Now().Format("2006-01-02") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid repeat rule."})
		return
	}
	tasks, ok := cachedTasks([]string{body.UUID})
	if !ok || asString(tasks[body.UUID], "status") != "open" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The to-do changed. Refresh and try again."})
		return
	}
	task := tasks[body.UUID]
	if asString(task, "repeat") != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "This to-do already repeats."})
		return
	}
	note := asString(task, "note")
	if !validText(note, 5000) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The notes are too long to copy into a repeating to-do."})
		return
	}
	repeat := map[string]any{"type": body.Type, "unit": body.Unit, "every": body.Every, "start": body.Start}
	if body.Month != "" {
		repeat["month"] = body.Month
	}
	create := map[string]any{"cmd": "create", "title": asString(task, "title"), "repeat": repeat}
	if note != "" {
		create["note"] = note
	}
	// A to-do in a project shows the project's area; only the project is
	// written, as Things does.
	if project := asString(task, "project"); project != "" {
		create["project"] = project
		if heading := asString(task, "heading"); heading != "" {
			create["heading"] = heading
		}
	} else if area := asString(task, "area"); area != "" {
		create["area"] = area
	}
	if tags := asStringSlice(task["tagIDs"]); len(tags) > 0 {
		create["tags"] = tags
	}
	ops := []map[string]any{create, {"cmd": "trash", "uuid": body.UUID}}
	ids, ok := writeBatch(w, ops, func([]string) func(map[string]any) map[string]any { return withoutTask(body.UUID) })
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uuid": firstString(ids)})
}
