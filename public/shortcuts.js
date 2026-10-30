"use strict";

// Keyboard shortcuts after Things for Mac
// (https://culturedcode.com/things/support/articles/2785159/). "Mod" is Cmd on
// a Mac and Ctrl elsewhere. Browsers keep a few combinations for themselves
// (new tab, new window, …); those shortcuts also work with Alt in place of
// Mod. The cheat sheet (?) is generated from this list.

const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

// Combinations browsers do not pass to pages.
const reservedCombos = new Set(["Mod+KeyN", "Mod+KeyT", "Mod+KeyW", "Mod+Shift+KeyN", "Mod+Shift+KeyT", "Mod+Shift+KeyW"]);

// Each entry: group, action, keys (combos with event.code names), when it
// applies (selection, editor, …) and what it does. Entries without run are
// listed in the cheat sheet as not available in the web app.
const shortcuts = [
  // Create
  { group: "Create", label: "New to-do", keys: ["Mod+KeyN", "Alt+KeyN", "KeyN"], run: () => focusQuickAdd() },
  { group: "Create", label: "New to-do below selection", keys: ["Space"], run: () => focusQuickAdd({ belowSelection: true }) },
  { group: "Create", label: "New to-dos from clipboard", keys: ["Mod+KeyV"], note: "Paste text: one to-do per line", passive: true },
  { group: "Create", label: "New checklist in open to-do", keys: ["Mod+Shift+KeyC", "Alt+Shift+KeyC"], editor: true, run: () => focusEditorField(".item-new") },
  { group: "Create", label: "New project", keys: ["Mod+Alt+KeyN"], run: () => openNewProject() },
  { group: "Create", label: "New heading", keys: ["Mod+Shift+KeyN", "Alt+Shift+KeyN"], run: () => newHeading() },

  // Edit
  { group: "Edit", label: "Open selected item", keys: ["Enter"], selection: 1, run: () => openEditor(firstSelected()) },
  { group: "Edit", label: "Save and close item", keys: ["Mod+Enter", "Escape"], editor: true, run: () => closeEditor() },
  { group: "Edit", label: "Duplicate selected item", keys: ["Mod+KeyD"], selection: true, run: () => duplicateSelected() },
  { group: "Edit", label: "Copy to-dos (titles)", keys: ["Mod+KeyC"], note: "Copies the titles as text", passive: true },
  { group: "Edit", label: "Complete selected item", keys: ["Mod+KeyK"], selection: true, editorToo: true, run: () => completeSelected() },
  { group: "Edit", label: "Cancel selected item", keys: ["Mod+Alt+KeyK"], selection: true, editorToo: true, run: () => cancelTasks(targetUUIDs()) },
  { group: "Edit", label: "Move completed to Logbook", keys: ["Mod+Shift+KeyY"], run: () => logCompleted() },
  { group: "Edit", label: "Move to Trash", keys: ["Delete", "Backspace"], selection: true, run: () => trashTasks(selectedInOrder()) },
  { group: "Edit", label: "Delete project selected in the sidebar", keys: ["Delete", "Backspace"], when: () => sidebarProject(), run: () => deleteProject(sidebarProject()) },

  // Select
  { group: "Select", label: "Select previous / next", keys: ["ArrowUp", "ArrowDown"], run: (event) => moveSelection(event.code === "ArrowDown" ? 1 : -1, false) },
  { group: "Select", label: "Select first", keys: ["Alt+ArrowUp"], run: () => selectEdge(-1) },
  { group: "Select", label: "Select last", keys: ["Alt+ArrowDown"], run: () => selectEdge(1) },
  { group: "Select", label: "Extend selection upwards", keys: ["Shift+ArrowUp"], run: () => moveSelection(-1, true) },
  { group: "Select", label: "Extend selection downwards", keys: ["Shift+ArrowDown"], run: () => moveSelection(1, true) },
  { group: "Select", label: "Extend selection to top", keys: ["Alt+Shift+ArrowUp"], run: () => extendToEdge(-1) },
  { group: "Select", label: "Extend selection to bottom", keys: ["Alt+Shift+ArrowDown"], run: () => extendToEdge(1) },
  { group: "Select", label: "Select all", keys: ["Mod+KeyA"], run: () => selectAll() },
  { group: "Select", label: "Clear selection", keys: ["Escape"], selection: true, run: () => clearSelection() },

  // Move
  { group: "Move", label: "Move item to another list", keys: ["Mod+Shift+KeyM"], selection: true, editorToo: true, run: () => openPalette("move") },
  { group: "Move", label: "Move item up", keys: ["Mod+ArrowUp"], selection: 1, run: () => moveSelected(-1) },
  { group: "Move", label: "Move item down", keys: ["Mod+ArrowDown"], selection: 1, run: () => moveSelected(1) },
  { group: "Move", label: "Move item to top of list", keys: ["Mod+Alt+ArrowUp"], selection: 1, run: () => moveSelectedToEdge(-1) },
  { group: "Move", label: "Move item to bottom of list", keys: ["Mod+Alt+ArrowDown"], selection: 1, run: () => moveSelectedToEdge(1) },

  // Dates
  { group: "When & Deadline", label: "Show When", keys: ["Mod+KeyS"], selection: 1, editorToo: true, run: () => focusEditorField(".edit-when", true) },
  { group: "When & Deadline", label: "Start Today", keys: ["Mod+KeyT", "Alt+KeyT"], selection: true, editorToo: true, run: () => scheduleSelected({ when: "today" }) },
  { group: "When & Deadline", label: "Start This Evening", keys: ["Mod+KeyE"], selection: true, editorToo: true, run: () => scheduleSelected({ when: "evening" }) },
  { group: "When & Deadline", label: "Start Anytime", keys: ["Mod+KeyR"], selection: true, editorToo: true, run: () => scheduleSelected({ when: "anytime" }) },
  { group: "When & Deadline", label: "Start Someday", keys: ["Mod+KeyO"], selection: true, editorToo: true, run: () => scheduleSelected({ when: "someday" }) },
  { group: "When & Deadline", label: "Start date +1 day", keys: ["Ctrl+BracketRight"], selection: true, editorToo: true, run: () => shiftStart(1) },
  { group: "When & Deadline", label: "Start date −1 day", keys: ["Ctrl+BracketLeft"], selection: true, editorToo: true, run: () => shiftStart(-1) },
  { group: "When & Deadline", label: "Start date +1 week", keys: ["Ctrl+Shift+BracketRight"], selection: true, editorToo: true, run: () => shiftStart(7) },
  { group: "When & Deadline", label: "Start date −1 week", keys: ["Ctrl+Shift+BracketLeft"], selection: true, editorToo: true, run: () => shiftStart(-7) },
  { group: "When & Deadline", label: "Add a deadline", keys: ["Mod+Shift+KeyD"], selection: 1, editorToo: true, run: () => focusEditorField(".edit-deadline", true) },
  { group: "When & Deadline", label: "Deadline +1 day", keys: ["Ctrl+Period"], selection: true, editorToo: true, run: () => shiftDeadline(1) },
  { group: "When & Deadline", label: "Deadline −1 day", keys: ["Ctrl+Comma"], selection: true, editorToo: true, run: () => shiftDeadline(-1) },
  { group: "When & Deadline", label: "Deadline +1 week", keys: ["Ctrl+Shift+Period"], selection: true, editorToo: true, run: () => shiftDeadline(7) },
  { group: "When & Deadline", label: "Deadline −1 week", keys: ["Ctrl+Shift+Comma"], selection: true, editorToo: true, run: () => shiftDeadline(-7) },
  { group: "When & Deadline", label: "Add repetition schedule", keys: ["Mod+Shift+KeyR"], selection: 1, editorToo: true, run: () => openRepeat(state.editing || firstSelected()) },

  // Tags
  { group: "Tags", label: "Edit tags for selected item", keys: ["Mod+Shift+KeyT", "Alt+Shift+KeyT"], selection: 1, editorToo: true, run: () => focusEditorField(".tag-new", true) },

  // Navigate
  { group: "Navigate", label: "Go to Inbox", keys: ["Mod+Digit1", "Digit1"], run: () => selectView("inbox") },
  { group: "Navigate", label: "Go to Today", keys: ["Mod+Digit2", "Digit2"], run: () => selectView("today") },
  { group: "Navigate", label: "Go to Upcoming", keys: ["Mod+Digit3", "Digit3"], run: () => selectView("upcoming") },
  { group: "Navigate", label: "Go to Anytime", keys: ["Mod+Digit4", "Digit4"], run: () => selectView("anytime") },
  { group: "Navigate", label: "Go to Someday", keys: ["Mod+Digit5", "Digit5"], run: () => selectView("someday") },
  { group: "Navigate", label: "Go to Logbook", keys: ["Mod+Digit6", "Digit6"], run: () => selectView("logbook") },
  { group: "Navigate", label: "Show navigation popover", keys: ["Mod+Shift+KeyO"], run: () => openPalette("go") },
  { group: "Navigate", label: "Show in parent list", keys: ["Mod+KeyL"], selection: 1, run: () => showInParent() },
  { group: "Navigate", label: "Go back", keys: ["Mod+ArrowLeft"], run: () => goBack() },
  { group: "Navigate", label: "Show/hide sidebar", keys: ["Mod+Slash"], run: () => document.body.classList.toggle("sidebar-hidden") },
  { group: "Navigate", label: "Search the app", keys: ["Mod+KeyF", "Char:/"], run: () => els.search.focus() },
  { group: "Navigate", label: "Refresh from Things Cloud", keys: ["KeyR"], note: "Web app only", run: () => sync({ fresh: true }) },
  { group: "Navigate", label: "Show keyboard shortcuts", keys: ["Char:?"], note: "Web app only", run: () => openCheatSheet() },

  // Text
  {
    group: "Text",
    label: "Bold, italic in notes",
    keys: ["Mod+KeyB", "Mod+KeyI"],
    editor: true,
    when: (event) => event.target.classList?.contains("edit-note"),
    run: (event) => wrapNoteSelection(event.target, event.code === "KeyB" ? "**" : "*"),
  },
];

// comboMatches tests a keydown against a combo like "Mod+Shift+KeyT" (keys by
// event.code, so they do not depend on the layout). "Char:?" matches a typed
// character instead, for keys that move between layouts (? and /).
function comboMatches(combo, event) {
  const parts = combo.split("+");
  const key = parts.pop();
  if (key.startsWith("Char:")) {
    return event.key === key.slice(5) && !event.ctrlKey && !event.metaKey && !event.altKey;
  }
  if (key !== event.code) return false;
  const want = new Set(parts);
  const expected = { metaKey: false, ctrlKey: false, altKey: want.has("Alt"), shiftKey: want.has("Shift") };
  if (want.has("Mod")) expected[isMac ? "metaKey" : "ctrlKey"] = true;
  if (want.has("Ctrl")) expected.ctrlKey = true;
  return Object.keys(expected).every((name) => Boolean(event[name]) === expected[name]);
}

// Key names for the cheat sheet, using the visitor's keyboard layout where
// the browser tells it.
let layoutMap = null;
navigator.keyboard?.getLayoutMap?.().then((map) => (layoutMap = map)).catch(() => {});

function keyName(code) {
  const named = {
    Enter: "Return", Escape: "Esc", Space: "Space", Delete: "Delete", Backspace: "⌫",
    ArrowUp: "↑", ArrowDown: "↓", ArrowLeft: "←", ArrowRight: "→",
  };
  if (named[code]) return named[code];
  if (layoutMap?.get(code)) return layoutMap.get(code).toUpperCase();
  const fallback = { BracketRight: "]", BracketLeft: "[", Period: ".", Comma: ",", Slash: "/" };
  if (fallback[code]) return fallback[code];
  return code.replace(/^Key|^Digit/, "");
}

function comboLabel(combo) {
  const parts = combo.split("+");
  const code = parts.pop();
  if (code.startsWith("Char:")) return `<kbd>${escapeHtml(code.slice(5))}</kbd>`;
  const names = isMac
    ? { Mod: "⌘", Ctrl: "⌃", Alt: "⌥", Shift: "⇧" }
    : { Mod: "Ctrl", Ctrl: "Ctrl", Alt: "Alt", Shift: "Shift" };
  const keys = [...parts.map((part) => names[part]), keyName(code)];
  return keys.map((key) => `<kbd>${escapeHtml(key)}</kbd>`).join(isMac ? "" : "+");
}

// typingTarget is an input where plain keys must type.
function typingTarget(target) {
  return target instanceof HTMLElement && (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName));
}

// handleShortcut runs the first shortcut matching a keydown.
function handleShortcut(event) {
  if (event.repeat && !event.code.startsWith("Arrow")) return;
  if (document.querySelector("dialog[open]")) return;
  if (event.key === "Escape" && event.target === els.search) {
    els.search.value = "";
    state.query = "";
    els.search.blur();
    renderTasks();
    return;
  }
  const typing = typingTarget(event.target);
  const inEditor = Boolean(event.target.closest?.(".task-editor"));
  const plain = !event.metaKey && !event.ctrlKey && !event.altKey;
  for (const shortcut of shortcuts) {
    if (!shortcut.run || shortcut.passive) continue;
    if (!shortcut.keys.some((combo) => comboMatches(combo, event))) continue;
    // In text fields only shortcuts with modifiers apply, and only those
    // meant for the open to-do or its selection.
    if (typing) {
      if (plain && !(shortcut.editor && event.code === "Escape")) continue;
      if (!inEditor || !(shortcut.editor || shortcut.editorToo)) continue;
    } else if (shortcut.editor && !state.editing) {
      continue;
    }
    if (shortcut.when && !shortcut.when(event)) continue;
    if (shortcut.selection && !targetUUIDs().length) continue;
    if (shortcut.selection === 1 && targetUUIDs().length !== 1) continue;
    event.preventDefault();
    event.stopPropagation();
    shortcut.run(event);
    return;
  }
}

// targetUUIDs are the to-dos a shortcut acts on: the open one, or the
// selection.
function targetUUIDs() {
  if (state.editing) return [state.editing];
  return selectedInOrder();
}

function selectedInOrder() {
  const rows = visibleRows();
  return rows.filter((uuid) => state.selected.has(uuid));
}

function firstSelected() {
  return targetUUIDs()[0];
}

function clearSelection() {
  state.selected.clear();
  renderSelection();
}

function selectEdge(direction) {
  const rows = visibleRows();
  if (!rows.length) return;
  select(direction < 0 ? rows[0] : rows[rows.length - 1]);
}

function extendToEdge(direction) {
  const rows = visibleRows();
  if (!rows.length) return;
  const anchor = state.anchor || rows[0];
  select(direction < 0 ? rows[0] : rows[rows.length - 1], { extend: true });
  state.anchor = anchor;
}

function selectAll() {
  const rows = visibleRows();
  state.selected = new Set(rows);
  state.anchor = rows[0] || "";
  renderSelection();
}

function moveSelectedToEdge(direction) {
  const uuid = firstSelected();
  const row = els.tasks.querySelector(`.task-row[data-task="${CSS.escape(uuid)}"]`);
  if (!row || !canReorder()) return;
  const order = [...row.parentElement.querySelectorAll(".task-row[data-task]")].map((item) => item.dataset.task).filter((id) => id !== uuid);
  if (direction < 0) order.unshift(uuid);
  else order.push(uuid);
  reorderTask(uuid, order);
}

function completeSelected() {
  for (const uuid of targetUUIDs()) toggleComplete(uuid);
}

function focusQuickAdd({ belowSelection = false } = {}) {
  if (els.quickAdd.hidden) return;
  state.insertAfter = belowSelection && state.selected.size ? selectedInOrder().at(-1) : "";
  els.quickAdd.elements.title.focus();
}

// focusEditorField opens the selected to-do if needed and focuses a field of
// its editor (title, when, deadline, tags, checklist).
function focusEditorField(selector, open = false) {
  if (!state.editing && open) openEditor(firstSelected());
  const field = els.tasks.querySelector(`.task-row.editing ${selector}`);
  if (!field) return;
  field.focus();
  if (field.tagName === "SELECT" || field.type === "date") field.showPicker?.();
}

function scheduleSelected(change) {
  const uuids = targetUUIDs();
  if (!uuids.length) return;
  const label = { today: "Today", evening: "This Evening", anytime: "Anytime", someday: "Someday" }[change.when];
  scheduleTasks(uuids, change).then(() => {
    if (state.editing) refreshEditorFooter(state.editing);
  });
  if (label) toast(uuids.length === 1 ? `“${findTask(uuids[0])?.title}” → ${label}` : `${uuids.length} to-dos → ${label}`, "success");
}

function addDays(date, days) {
  const result = new Date(`${date}T00:00:00`);
  result.setDate(result.getDate() + days);
  return isoDay(result);
}

// shiftStart moves the start date of the to-dos by days; a to-do without a
// start date starts from today.
function shiftStart(days) {
  const today = isoDay(new Date());
  const groups = new Map();
  for (const uuid of targetUUIDs()) {
    const task = findTask(uuid);
    if (!task) continue;
    const from = task.scheduledDate || today;
    let date = addDays(from, days);
    if (date < today) date = today;
    if (!groups.has(date)) groups.set(date, []);
    groups.get(date).push(uuid);
  }
  for (const [date, uuids] of groups) {
    scheduleTasks(uuids, date === today ? { when: "today" } : { startDate: date }).then(() => {
      if (state.editing) refreshEditorFooter(state.editing);
    });
  }
}

function shiftDeadline(days) {
  const today = isoDay(new Date());
  for (const uuid of targetUUIDs()) {
    const task = findTask(uuid);
    if (task) saveDeadline(uuid, addDays(task.deadline || today, days));
  }
}

// duplicateSelected copies the to-dos (title, notes, place, deadline, tags
// and checklist) right below the originals.
async function duplicateSelected() {
  const tasks = targetUUIDs().map(findTask).filter(Boolean);
  for (const task of tasks) {
    const views = task.views || [];
    const payload = { title: task.title, note: task.note || "" };
    if (task.heading) payload.heading = task.heading;
    if (task.project) payload.project = task.project;
    else if (task.area) payload.area = task.area;
    if (views.includes("upcoming") && task.scheduledDate) payload.scheduled = task.scheduledDate;
    else payload.when = views.includes("today") ? "today" : views.includes("inbox") ? "inbox" : views.includes("someday") ? "someday" : "anytime";
    if (payload.heading) delete payload.project;
    try {
      const result = await api("/api/create", { method: "POST", body: JSON.stringify(payload) });
      const uuid = result.uuid;
      if (task.deadline) await api("/api/deadline", { method: "POST", body: JSON.stringify({ uuid, deadline: task.deadline }) });
      if (task.tagIDs?.length) await api("/api/tags", { method: "POST", body: JSON.stringify({ uuid, tags: task.tagIDs }) });
      for (const item of task.checklist || []) {
        await api("/api/checklist", { method: "POST", body: JSON.stringify({ action: "add", task: uuid, title: item.title }) });
      }
    } catch (error) {
      toast(`Could not duplicate “${task.title}”: ${error.message}`, "error");
      return;
    }
  }
  toast(tasks.length === 1 ? `Duplicated “${tasks[0].title}”` : `Duplicated ${tasks.length} to-dos`, "success");
  sync({ fresh: true, quiet: true });
}

// showInParent opens the project or area of the selected to-do.
function showInParent() {
  const task = findTask(firstSelected());
  if (!task) return;
  const scope = task.project
    ? { type: "project", id: task.project, title: task.projectTitle }
    : task.area
      ? { type: "area", id: task.area, title: task.areaTitle }
      : null;
  if (!scope) return;
  rememberPlace();
  state.scope = scope;
  document.querySelectorAll(".nav-item").forEach((nav) => nav.classList.remove("active"));
  state.selected = new Set([task.uuid]);
  render();
}

// History of places for "Go back".
const placeHistory = [];

function rememberPlace() {
  placeHistory.push({ view: state.view, scope: state.scope });
  if (placeHistory.length > 50) placeHistory.shift();
}

function goBack() {
  const place = placeHistory.pop();
  if (!place) return;
  closeEditor();
  state.view = place.view;
  state.scope = place.scope;
  document.querySelectorAll(".nav-item").forEach((item) => item.classList.toggle("active", !place.scope && item.dataset.view === place.view));
  render();
}

// Palette: a small search window for "Move item to another list" and the
// navigation popover.
function paletteItems(mode) {
  const items = [];
  const lists = mode === "move" ? ["inbox", "today", "anytime", "someday"] : ["inbox", "today", "upcoming", "anytime", "someday", "logbook", "trash"];
  for (const view of lists) items.push({ kind: "list", id: view, title: viewTitles[view], icon: icons[view] });
  const areas = state.model?.areas || [];
  // Completed and canceled projects take nothing new.
  const projects = liveProjects().filter((project) => mode !== "move" || (project.status || "open") === "open");
  for (const project of projects.sort(byIndex)) {
    const area = areas.find((item) => item.uuid === project.area);
    items.push({ kind: "project", id: project.uuid, title: project.title, hint: area?.title || "", icon: icons.project });
    if (mode === "move") {
      for (const heading of (state.model?.headings || []).filter((item) => item.project === project.uuid).sort(byIndex)) {
        items.push({ kind: "heading", id: heading.uuid, title: heading.title, hint: project.title, parent: project.uuid, icon: "" });
      }
    }
  }
  for (const area of areas.slice().sort(byIndex)) items.push({ kind: "area", id: area.uuid, title: area.title, icon: icons.area });
  return items;
}

function openPalette(mode) {
  const uuids = mode === "move" ? targetUUIDs() : [];
  if (mode === "move" && !uuids.length) return;
  closePalette();
  const overlay = document.createElement("div");
  overlay.className = "palette-overlay";
  overlay.innerHTML = `
    <div class="palette" role="dialog" aria-label="${mode === "move" ? "Move to" : "Go to"}">
      <input class="palette-input" placeholder="${mode === "move" ? (uuids.length === 1 ? "Move to…" : `Move ${uuids.length} to-dos to…`) : "Go to…"}" aria-label="Search lists, projects and areas" />
      <div class="palette-list" role="listbox"></div>
    </div>`;
  document.body.append(overlay);
  const input = overlay.querySelector(".palette-input");
  const list = overlay.querySelector(".palette-list");
  const all = paletteItems(mode);
  let shown = all;
  let active = 0;
  const draw = () => {
    const query = input.value.trim().toLowerCase();
    shown = all.filter((item) => !query || `${item.title} ${item.hint || ""}`.toLowerCase().includes(query)).slice(0, 50);
    active = Math.min(active, Math.max(0, shown.length - 1));
    // A heading is indented only right below its own project; otherwise it
    // names the project.
    let project = "";
    list.innerHTML = shown
      .map((item, i) => {
        if (item.kind === "project") project = item.id;
        else if (item.kind !== "heading") project = "";
        const nested = item.kind === "heading" && item.parent === project;
        const hint = item.kind === "heading" ? (nested ? "" : `in ${item.hint}`) : item.hint || "";
        return `<button type="button" class="palette-item${i === active ? " active" : ""}${nested ? " nested" : ""}${item.kind === "heading" ? " heading" : ""}" data-index="${i}" role="option">
          <span class="nav-icon">${item.icon || ""}</span><span class="mini-title">${escapeHtml(item.title)}</span><span class="palette-hint">${escapeHtml(hint)}</span>
        </button>`;
      })
      .join("");
  };
  const choose = (item) => {
    closePalette();
    if (!item) return;
    if (mode === "go") {
      rememberPlace();
      if (item.kind === "list") selectView(item.id);
      else {
        closeEditor();
        state.scope = { type: item.kind, id: item.id, title: item.title };
        document.querySelectorAll(".nav-item").forEach((nav) => nav.classList.remove("active"));
        render();
      }
      return;
    }
    if (item.kind === "list") {
      if (item.id === "inbox") scheduleTasks(uuids, { when: "inbox" });
      else moveTasks(uuids, { when: item.id, title: item.title });
    } else {
      moveTasks(uuids, { [item.kind]: item.id, title: item.title });
    }
  };
  input.addEventListener("input", () => {
    active = 0;
    draw();
  });
  input.addEventListener("keydown", (event) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      active = (active + (event.key === "ArrowDown" ? 1 : -1) + shown.length) % Math.max(1, shown.length);
      draw();
      list.querySelector(".active")?.scrollIntoView({ block: "nearest" });
    } else if (event.key === "Enter") {
      event.preventDefault();
      choose(shown[active]);
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closePalette();
    }
  });
  list.addEventListener("click", (event) => {
    const button = event.target.closest(".palette-item");
    if (button) choose(shown[Number(button.dataset.index)]);
  });
  overlay.addEventListener("click", (event) => {
    if (event.target === overlay) closePalette();
  });
  draw();
  input.focus();
}

function closePalette() {
  document.querySelector(".palette-overlay")?.remove();
}

// Cheat sheet of all shortcuts, generated from the list above.
function openCheatSheet() {
  closePalette();
  let dialog = document.querySelector("#shortcuts-dialog");
  if (!dialog) {
    dialog = document.createElement("dialog");
    dialog.id = "shortcuts-dialog";
    dialog.className = "cheat-sheet";
    document.body.append(dialog);
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog || event.target.closest("[data-close]")) dialog.close();
    });
  }
  const groups = new Map();
  for (const shortcut of shortcuts) {
    if (!groups.has(shortcut.group)) groups.set(shortcut.group, []);
    groups.get(shortcut.group).push(shortcut);
  }
  const keysFor = (shortcut) => {
    const usable = shortcut.keys.filter((combo) => !reservedCombos.has(combo));
    const shown = (usable.length ? usable : shortcut.keys).slice(0, 2);
    return shown.map(comboLabel).join(" <span class=\"or\">or</span> ");
  };
  const noteFor = (shortcut) => {
    if (!shortcut.run && !shortcut.passive) return "Not available in the web app";
    const reserved = shortcut.keys.filter((combo) => reservedCombos.has(combo));
    if (reserved.length) return `${comboLabel(reserved[0])} is kept by the browser`;
    return shortcut.note || "";
  };
  dialog.innerHTML = `
    <div class="cheat-head">
      <h2>Keyboard shortcuts</h2>
      <button type="button" class="icon-button" data-close aria-label="Close">×</button>
    </div>
    <p class="cheat-intro">After <a href="https://culturedcode.com/things/support/articles/2785159/" target="_blank" rel="noopener noreferrer">Things for Mac</a>. ${isMac ? "⌘ is Cmd" : "Ctrl stands for Cmd"}; where the browser keeps a combination, use ${isMac ? "⌥" : "Alt"} instead.</p>
    <div class="cheat-grid">
      ${[...groups]
        .map(
          ([group, items]) => `
        <section>
          <h3>${escapeHtml(group)}</h3>
          <table>
            ${items
              .map(
                (shortcut) => `<tr class="${shortcut.run || shortcut.passive ? "" : "unavailable"}">
                  <td>${escapeHtml(shortcut.label)}${noteFor(shortcut) ? `<div class="cheat-note">${noteFor(shortcut)}</div>` : ""}</td>
                  <td class="keys">${keysFor(shortcut)}</td>
                </tr>`,
              )
              .join("")}
          </table>
        </section>`,
        )
        .join("")}
    </div>`;
  dialog.showModal();
}

// Copy and paste: titles of the selected to-dos as text, and new to-dos from
// pasted lines.
document.addEventListener("copy", (event) => {
  if (typingTarget(event.target) || state.editing || !state.selected.size) return;
  const titles = selectedInOrder().map((uuid) => findTask(uuid)?.title).filter(Boolean);
  if (!titles.length) return;
  event.clipboardData.setData("text/plain", titles.join("\n"));
  event.preventDefault();
  toast(titles.length === 1 ? "Copied the to-do" : `Copied ${titles.length} to-dos`);
});

document.addEventListener("paste", (event) => {
  if (typingTarget(event.target) || state.editing || els.quickAdd.hidden) return;
  const lines = (event.clipboardData?.getData("text/plain") || "")
    .split(/\r?\n/)
    .map((line) => line.replace(/^\s*(?:[-*•]|\[ ?\]|\d+[.)])\s+/, "").trim())
    .filter(Boolean)
    .slice(0, 50);
  if (!lines.length) return;
  event.preventDefault();
  for (const line of lines.reverse()) {
    els.quickAdd.elements.title.value = line;
    createTask(els.quickAdd);
  }
});
