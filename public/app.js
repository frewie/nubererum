const state = {
  model: null,
  // loading: waiting for the first model; signedOut: no Things account; ready.
  phase: "loading",
  view: "today",
  query: "",
  scope: null,
  // Status changes made here (uuid → {status, at}) until the server model
  // shows them.
  overrides: new Map(),
  // To-dos and projects moved to the Trash or logged here, hidden until the
  // model drops them.
  hidden: new Map(),
  // Selected to-dos, and the one a Shift selection extends from.
  selected: new Set(),
  anchor: "",
  // Tasks being created, shown until a sync after the create.
  pending: [],
  // Created tasks (uuid → {task, at}) kept until a sync after the create.
  created: new Map(),
  syncedAt: null,
  syncError: "",
  // Logbook and Trash entries, loaded when those lists open.
  archive: { logbook: null, trash: null },
  // The to-do open in the editor.
  editing: "",
  // A task list render waits while the editor is in use.
  renderWaiting: false,
};

const viewTitles = {
  today: "Today",
  inbox: "Inbox",
  anytime: "Anytime",
  upcoming: "Upcoming",
  someday: "Someday",
  logbook: "Logbook",
  trash: "Trash",
};

// Logbook and Trash are loaded on demand, not part of the synced model.
const archiveViews = ["logbook", "trash"];

// Things' order, which the 1–5 shortcuts follow.
const viewKeys = ["inbox", "today", "upcoming", "anytime", "someday"];

// Icons drawn for this app in the spirit of Things' list icons.
const icons = {
  evening: `<svg class="icon" viewBox="0 0 20 20"><path d="M14.6 13.4A6.6 6.6 0 0 1 8.2 3.1a6.9 6.9 0 1 0 8.7 8.6 6.6 6.6 0 0 1-2.3 1.7z" fill="#6f8fc9" stroke="#56739f" stroke-width=".8" stroke-linejoin="round"/></svg>`,
  inbox: `<svg class="icon" viewBox="0 0 20 20"><rect x="2" y="3" width="16" height="14" rx="3.5" fill="#2b84e6"/><path d="M2.8 11.2h4l1.4 2.3h3.6l1.4-2.3h4" fill="none" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/></svg>`,
  today: `<svg class="icon" viewBox="0 0 20 20"><path d="M10 1.8l2.45 5.05 5.55.78-4.03 3.9.96 5.52L10 14.43l-4.93 2.62.96-5.52L2 7.63l5.55-.78z" fill="#f5c211" stroke="#e0a800" stroke-width=".8" stroke-linejoin="round"/></svg>`,
  upcoming: `<svg class="icon" viewBox="0 0 20 20"><rect x="2.5" y="3.5" width="15" height="13.5" rx="3" fill="#fff" stroke="#e5484d" stroke-width="1.6"/><path d="M2.5 7.2h15" stroke="#e5484d" stroke-width="2.6"/><g fill="#e5484d"><circle cx="6.5" cy="10.6" r="1"/><circle cx="10" cy="10.6" r="1"/><circle cx="13.5" cy="10.6" r="1"/><circle cx="6.5" cy="13.8" r="1"/><circle cx="10" cy="13.8" r="1"/></g></svg>`,
  anytime: `<svg class="icon" viewBox="0 0 20 20"><path d="M10 2.5l7.5 3.8L10 10 2.5 6.3z" fill="#2a9d8f"/><path d="M3.4 9.6 10 13l6.6-3.4M3.4 13 10 16.4l6.6-3.4" fill="none" stroke="#2a9d8f" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  someday: `<svg class="icon" viewBox="0 0 20 20"><rect x="2" y="3.5" width="16" height="4" rx="1.3" fill="#d4b26a"/><path d="M3.3 7.5h13.4v7.6a2 2 0 0 1-2 2H5.3a2 2 0 0 1-2-2z" fill="#ead39c"/><path d="M8 10.5h4" stroke="#a8843c" stroke-width="1.5" stroke-linecap="round"/></svg>`,
  project: `<svg class="icon" viewBox="0 0 20 20"><circle cx="10" cy="10" r="7" fill="none" stroke="#7d8ea8" stroke-width="1.7"/><path d="M10 5.2a4.8 4.8 0 0 1 4.8 4.8H10z" fill="#7d8ea8"/></svg>`,
  area: `<svg class="icon" viewBox="0 0 20 20"><path d="M10 2.6 16.6 6v8L10 17.4 3.4 14V6z" fill="none" stroke="#7d8492" stroke-width="1.5" stroke-linejoin="round"/><path d="M3.6 6.1 10 9.4l6.4-3.3M10 9.4v7.8" fill="none" stroke="#7d8492" stroke-width="1.5" stroke-linejoin="round"/></svg>`,
  plus: `<svg class="icon" viewBox="0 0 20 20"><path d="M10 4v12M4 10h12" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`,
  settings: `<svg class="icon" viewBox="0 0 20 20"><circle cx="10" cy="10" r="2.6" fill="none" stroke="currentColor" stroke-width="1.6"/><path d="M10 2.5v2.2M10 15.3v2.2M2.5 10h2.2M15.3 10h2.2M4.7 4.7l1.6 1.6M13.7 13.7l1.6 1.6M4.7 15.3l1.6-1.6M13.7 6.3l1.6-1.6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`,
  key: `<svg class="icon" viewBox="0 0 20 20"><circle cx="7" cy="10" r="3.6" fill="none" stroke="currentColor" stroke-width="1.6"/><path d="M10.6 10H17M14.5 10v3M17 10v2.2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`,
  search: `<svg class="icon" viewBox="0 0 20 20"><circle cx="8.6" cy="8.6" r="5.2" fill="none" stroke="currentColor" stroke-width="1.7"/><path d="m12.6 12.6 4.4 4.4" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>`,
  refresh: `<svg class="icon" viewBox="0 0 20 20"><path d="M16 10a6 6 0 1 1-1.8-4.3M16 3.6v3.6h-3.6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  note: `<svg class="icon" viewBox="0 0 20 20"><path d="M4 5h12M4 10h12M4 15h7" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`,
  tag: `<svg class="icon" viewBox="0 0 20 20"><path d="M3 3.5h6.5l7.5 7.5-6 6-7.5-7.5z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/><circle cx="7" cy="7.5" r="1.3" fill="currentColor"/></svg>`,
  logbook: `<svg class="icon" viewBox="0 0 20 20"><rect x="3" y="2.5" width="14" height="15" rx="2.5" fill="#3aa65b"/><path d="M6.5 10.2l2.4 2.4 4.6-5" fill="none" stroke="#fff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  trash: `<svg class="icon" viewBox="0 0 20 20"><path d="M4.5 6h11l-1 10.5a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5z" fill="#c3c8d0"/><path d="M3.5 5h13M8 5V3.5h4V5" fill="none" stroke="#8b929c" stroke-width="1.5" stroke-linecap="round"/></svg>`,
  keyboard: `<svg class="icon" viewBox="0 0 20 20"><rect x="2" y="5" width="16" height="10" rx="2" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M5 8.5h1M8 8.5h1M11 8.5h1M14 8.5h1M6 11.5h8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>`,
  checklist: `<svg class="icon" viewBox="0 0 20 20"><path d="M3.5 5.5l1.5 1.5 2.5-3M3.5 13l1.5 1.5 2.5-3M10 5.5h6.5M10 13h6.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  bell: `<svg class="icon" viewBox="0 0 20 20"><path d="M5.5 14V9a4.5 4.5 0 0 1 9 0v5l1.5 1.5h-12z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/><path d="M8.3 17.2a1.8 1.8 0 0 0 3.4 0" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`,
  flag: `<svg class="icon" viewBox="0 0 20 20"><path d="M5 17V3.5M5 4h9l-2 3.5 2 3.5H5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
};

function fillIcons(root = document) {
  root.querySelectorAll("[data-icon]").forEach((element) => {
    element.innerHTML = icons[element.dataset.icon] || "";
  });
}

// Refresh in the background when the page comes back after this long, and
// every refreshEvery while it is visible.
const staleAfter = 60 * 1000;
const refreshEvery = 5 * 60 * 1000;

const els = {
  account: document.querySelector("#account-link"),
  backendNote: document.querySelector("#backend-note"),
  emptySignin: document.querySelector("#empty-signin"),
  loading: document.querySelector("#loading"),
  loginForm: document.querySelector("#login-form"),
  logCompleted: document.querySelector("#log-completed"),
  newHeading: document.querySelector("#new-heading"),
  deleteProject: document.querySelector("#delete-project"),
  completeProject: document.querySelector("#complete-project"),
  projectDetails: document.querySelector("#project-details"),
  projectFinish: document.querySelector("#project-finish"),
  repeatDialog: document.querySelector("#repeat-dialog"),
  tagsDialog: document.querySelector("#tags-dialog"),
  tagsButton: document.querySelector("#tags-button"),
  projectNote: document.querySelector("#project-note"),
  newProject: document.querySelector("#new-project"),
  newProjectButton: document.querySelector("#new-project-button"),
  newAreaButton: document.querySelector("#new-area-button"),
  shortcutsButton: document.querySelector("#shortcuts-button"),
  projects: document.querySelector("#projects"),
  quickAdd: document.querySelector("#quick-add"),
  refresh: document.querySelector("#refresh"),
  search: document.querySelector("#search"),
  settings: document.querySelector("#settings-button"),
  status: document.querySelector("#status"),
  statusText: document.querySelector("#status-text"),
  summary: document.querySelector("#summary"),
  tasks: document.querySelector("#tasks"),
  toasts: document.querySelector("#toasts"),
  viewIcon: document.querySelector("#view-icon"),
  viewTitle: document.querySelector("#view-title"),
};

// reportError sends browser errors to the server log (at most 20 per page).
let reportsLeft = 20;
function reportError(details) {
  if (reportsLeft-- <= 0) return;
  try {
    fetch("/api/client-error", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ page: location.pathname + location.search, ...details }),
      keepalive: true,
    }).catch(() => {});
  } catch {
    // Reporting must never break the page.
  }
}

window.addEventListener("error", (event) => {
  reportError({
    message: event.message,
    source: event.filename,
    line: event.lineno,
    stack: event.error?.stack || "",
    context: "error",
  });
});
window.addEventListener("unhandledrejection", (event) => {
  reportError({ message: String(event.reason?.message || event.reason), stack: event.reason?.stack || "", context: "unhandledrejection" });
});

async function api(path, options = {}) {
  let response;
  try {
    response = await fetch(path, {
      ...options,
      headers: {
        "content-type": "application/json",
        ...(options.headers || {}),
      },
    });
  } catch (error) {
    // The server never saw this request, so report it from here.
    reportError({ message: error.message, context: `network ${options.method || "GET"} ${path}` });
    throw new Error("The server is not reachable. Check the connection and try again.");
  }
  const data = await response.json();
  // Passkey mode: the session ended, so sign in again.
  if (response.status === 401 && data.login) {
    location.href = data.login;
    throw new Error(data.error);
  }
  if (!response.ok) throw new Error(data.error || "Request failed.");
  return data;
}

function setStatus(text, connected = false, busy = false) {
  els.status.classList.toggle("connected", connected);
  els.status.classList.toggle("busy", busy);
  els.statusText.textContent = text;
}

function timeLabel(date) {
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function showSyncState() {
  if (state.syncing) {
    setStatus(state.model ? "Updating…" : "Loading…", Boolean(state.model), true);
  } else if (state.syncError) {
    setStatus("Sync failed", false);
  } else if (state.syncedAt) {
    setStatus(`Synced ${timeLabel(state.syncedAt)}`, true);
  }
  els.refresh.classList.toggle("spinning", Boolean(state.syncing));
}

function toast(message, kind = "info", action = null) {
  const item = document.createElement("div");
  item.className = `toast ${kind}`;
  item.textContent = message;
  if (action) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "toast-action";
    button.textContent = action.label;
    button.addEventListener("click", () => {
      item.remove();
      action.run();
    });
    item.append(" ", button);
  }
  els.toasts.append(item);
  setTimeout(
    () => {
      item.classList.add("leaving");
      setTimeout(() => item.remove(), 300);
    },
    action ? 8000 : kind === "error" ? 6000 : 3000,
  );
}

function setBackendNote(backend) {
  if (!backend || backend.cli) {
    els.backendNote.classList.remove("visible");
    els.backendNote.textContent = "";
    return;
  }
  els.backendNote.classList.add("visible");
  els.backendNote.textContent =
    "things-cli is missing on the server. Install it to sync your Things Cloud account.";
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function renderSafeLink(raw) {
  const href = raw.toLowerCase().startsWith("www.") ? `https://${raw}` : raw;
  try {
    const url = new URL(href);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return escapeHtml(raw);
    }
    return `<a href="${escapeHtml(url.href)}" target="_blank" rel="noopener noreferrer">${escapeHtml(raw)}</a>`;
  } catch {
    return escapeHtml(raw);
  }
}

function renderNote(note) {
  const text = String(note ?? "");
  const linkPattern = /(https?:\/\/[^\s<>"']+|www\.[^\s<>"']+)/gi;
  let html = "";
  let lastIndex = 0;
  for (const match of text.matchAll(linkPattern)) {
    const raw = match[0];
    const index = match.index ?? 0;
    const trimmed = raw.replace(/[),.;\]]+$/g, "");
    const trailing = raw.slice(trimmed.length);
    html += escapeHtml(text.slice(lastIndex, index));
    html += renderSafeLink(trimmed);
    html += escapeHtml(trailing);
    lastIndex = index + raw.length;
  }
  html += escapeHtml(text.slice(lastIndex));
  return html;
}

function formatDate(value) {
  if (!value) return "";
  const date = new Date(`${value}T00:00:00`);
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    year: date.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
  }).format(date);
}

function isDone(task) {
  return task.status === "completed" || task.status === "canceled";
}

// visible applies changes made here before the server model shows them: it
// hides trashed and logged to-dos and uses local completion states.
function visible(tasks) {
  return tasks
    .filter((task) => !state.hidden.has(task.uuid))
    .map((task) => {
      const override = state.overrides.get(task.uuid);
      return override && override.status !== task.status ? { ...task, status: override.status } : task;
    });
}

function pendingTasks() {
  return state.pending.filter((task) => {
    if (state.scope?.type === "project") return task.project === state.scope.id;
    if (state.scope?.type === "area") return task.area === state.scope.id;
    return !task.project && !task.area && task.views.includes(state.view);
  });
}

function isoDay(date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

// dayLabel names a start date the way Things groups Upcoming.
function dayLabel(value) {
  if (!value) return "Repeating";
  const date = new Date(`${value}T00:00:00`);
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const days = Math.round((date - today) / 86400000);
  if (days <= 0) return "Today";
  if (days === 1) return "Tomorrow";
  if (days < 7) return new Intl.DateTimeFormat(undefined, { weekday: "long" }).format(date);
  return formatDate(value);
}

// listKey names the list on screen; a tag filter belongs to one list.
function listKey() {
  return state.scope ? `${state.scope.type}:${state.scope.id}` : state.view;
}

// activeTagFilter is the tag the list on screen is filtered by, or "".
function activeTagFilter() {
  return state.tagFilter?.list === listKey() ? state.tagFilter.uuid : "";
}

// listTasks returns the to-dos of the list on screen, filtered by the search
// and, unless withoutTagFilter, by the tag filter.
function listTasks(withoutTagFilter = false) {
  if (!state.model) return [];
  let base = state.model.lists[state.view] || [];
  if (state.scope?.type === "project") {
    base = state.model.tasks.filter((task) => task.project === state.scope.id);
  } else if (state.scope?.type === "area") {
    base = state.model.tasks.filter((task) => task.area === state.scope.id);
  }
  // New to-dos start at the top, as in Things.
  base = [...pendingTasks(), ...visible(base)];
  // The open to-do stays until it is closed, even when it no longer belongs
  // here (e.g. after moving it to Someday).
  if (state.editing && !base.some((task) => task.uuid === state.editing)) {
    const open = findTask(state.editing);
    if (open) base = [open, ...base];
  }
  const tag = withoutTagFilter ? "" : activeTagFilter();
  if (tag) base = base.filter((task) => inheritedTagIDs(task).includes(tag) || task.uuid === state.editing);
  const query = state.query.trim().toLowerCase();
  if (!query) return base;
  return base.filter((task) =>
    [task.title, task.note, task.projectTitle, task.areaTitle, ...(task.tags || [])]
      .join(" ")
      .toLowerCase()
      .includes(query),
  );
}

// groupTasks follows Things: loose to-dos first without a heading, then one
// group per project or area; projects by heading; Upcoming by day.
function groupTasks(tasks) {
  if (state.scope?.type === "project") {
    // Loose to-dos, then every heading of the project in Things' order, also
    // empty ones (they are drop targets).
    const headings = (state.model.headings || []).filter((heading) => heading.project === state.scope.id).sort(byIndex);
    const known = new Set(headings.map((heading) => heading.uuid));
    return [
      { title: "", kind: "", heading: "", tasks: tasks.filter((task) => !known.has(task.heading)) },
      ...headings.map((heading) => ({
        title: heading.title,
        kind: "heading",
        heading: heading.uuid,
        tasks: tasks.filter((task) => task.heading === heading.uuid),
      })),
    ];
  }
  if (state.scope?.type === "area") {
    return groupBy(tasks, (task) => task.projectTitle || "", () => "project");
  }
  if (state.view === "upcoming") {
    return groupBy(tasks, (task) => dayLabel(task.scheduledDate || task.todayDate), () => "day");
  }
  if (state.view === "today") {
    // This Evening comes last, as in Things.
    const byProject = (rows) =>
      groupBy(
        rows,
        (task) => task.projectTitle || task.areaTitle || "",
        (task) => (task.projectTitle ? "project" : "area"),
      );
    const evening = tasks.filter((task) => task.evening);
    const groups = byProject(tasks.filter((task) => !task.evening));
    if (evening.length) groups.push({ title: "This Evening", kind: "evening", evening: true, tasks: evening });
    return groups;
  }
  if (["today", "anytime", "someday"].includes(state.view)) {
    return groupBy(
      tasks,
      (task) => task.projectTitle || task.areaTitle || "",
      (task) => (task.projectTitle ? "project" : "area"),
    );
  }
  return [{ title: "", kind: "", tasks }];
}

function groupBy(tasks, titleForTask, kindForTask) {
  const groups = new Map();
  for (const task of tasks) {
    const title = titleForTask(task);
    if (!groups.has(title)) groups.set(title, { title, kind: title ? kindForTask(task) : "", tasks: [] });
    groups.get(title).tasks.push(task);
  }
  // Loose to-dos come first, as in Things.
  return [...groups.values()].sort((a, b) => (a.title ? 1 : 0) - (b.title ? 1 : 0));
}

function renderCounts() {
  for (const view of viewKeys) {
    const count = visible(state.model?.lists?.[view] || []).filter((task) => !isDone(task)).length;
    document.querySelector(`#count-${view}`).textContent = count;
  }
}

function byIndex(a, b) {
  return (a.index ?? 0) - (b.index ?? 0) || String(a.title).localeCompare(String(b.title));
}

// collapsedAreas are the areas folded up in the sidebar, remembered per
// browser; a missing or broken value means all are open.
const collapsedKey = "things-collapsed-areas";
const collapsedAreas = (() => {
  try {
    const saved = JSON.parse(localStorage.getItem(collapsedKey));
    return new Set(Array.isArray(saved) ? saved.filter((uuid) => typeof uuid === "string") : []);
  } catch {
    return new Set();
  }
})();

// toggleArea folds an area up or opens it, like Things.
function toggleArea(uuid, open = collapsedAreas.has(uuid)) {
  if (open === !collapsedAreas.has(uuid)) return;
  if (open) collapsedAreas.delete(uuid);
  else collapsedAreas.add(uuid);
  try {
    localStorage.setItem(collapsedKey, JSON.stringify([...collapsedAreas]));
  } catch {
    // Storage is optional.
  }
  renderSidebar();
  els.projects.querySelector(`.area-row[data-scope-id="${CSS.escape(uuid)}"]`)?.focus();
}

// sidebarRows mirrors the Things sidebar: projects without an area, then each
// area with its projects, in Things' order. It lists active projects only;
// Someday and Upcoming projects appear in those lists.
function sidebarRows() {
  const projects = liveProjects().filter((project) => (project.when || "anytime") === "anytime");
  const areas = state.model?.areas || [];
  const row = (type, item, depth) => ({ type, item, depth, count: item.count || 0 });
  const areaIds = new Set(areas.map((area) => area.uuid));
  const rows = projects
    .filter((project) => !areaIds.has(project.area))
    .sort(byIndex)
    .map((project) => row("project", project, 0));
  for (const area of [...areas].sort(byIndex)) {
    rows.push(row("area", area, 0));
    if (collapsedAreas.has(area.uuid)) continue;
    for (const project of projects.filter((item) => item.area === area.uuid).sort(byIndex)) {
      rows.push(row("project", project, 1));
    }
  }
  return rows;
}

// liveProjects are the model's projects without those just deleted here.
function liveProjects() {
  return (state.model?.projects || []).filter((project) => !state.hidden.has(project.uuid));
}

// isTemp tells items shown before Things Cloud returned their id; nothing
// may be created in or moved to them yet.
function isTemp(uuid) {
  return String(uuid || "").startsWith("pending-");
}

// projectPie draws a project's progress like Things: a ring that fills with
// the share of completed to-dos; a tick when the project is completed, a
// cross when canceled.
function projectPie(project) {
  if (project.status === "completed" || project.status === "canceled") {
    const mark = project.status === "completed" ? "M6.3 10.2 8.8 12.6 13.8 7.4" : "M7.2 7.2l5.6 5.6M12.8 7.2l-5.6 5.6";
    const fill = project.status === "completed" ? "#7d8ea8" : "#a7adb6";
    return `<svg class="icon pie" viewBox="0 0 20 20" data-progress="${project.status}"><circle cx="10" cy="10" r="7.5" fill="${fill}"/><path d="${mark}" fill="none" stroke="#fff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
  }
  const total = project.total || 0;
  const share = total ? Math.min(1, (project.done || 0) / total) : 0;
  let slice = "";
  if (share >= 1) slice = `<circle cx="10" cy="10" r="5" fill="#7d8ea8"/>`;
  else if (share > 0) {
    const angle = share * 2 * Math.PI;
    const x = 10 + 5 * Math.sin(angle);
    const y = 10 - 5 * Math.cos(angle);
    slice = `<path d="M10 10V5A5 5 0 ${share > 0.5 ? 1 : 0} 1 ${x.toFixed(2)} ${y.toFixed(2)}Z" fill="#7d8ea8"/>`;
  }
  return `<svg class="icon pie" viewBox="0 0 20 20" data-progress="${project.done || 0}/${total}"><circle cx="10" cy="10" r="7" fill="none" stroke="#7d8ea8" stroke-width="1.6"/>${slice}</svg>`;
}

function scopeButton(type, item, extraClass = "") {
  if (isTemp(item.uuid)) return `<button type="button" class="${extraClass} pending" disabled>`;
  const active = state.scope?.type === type && state.scope?.id === item.uuid ? " active" : "";
  return `<button type="button" class="${extraClass}${active}" data-scope-type="${type}" data-scope-id="${escapeHtml(item.uuid)}" data-scope-title="${escapeHtml(item.title)}">`;
}

function renderSidebar() {
  // Wait while a name is edited in the sidebar.
  if (els.projects.querySelector(".rename-input")) {
    state.sidebarWaiting = true;
    return;
  }
  state.sidebarWaiting = false;
  const rows = sidebarRows();
  if (!rows.length) {
    els.projects.innerHTML = `<div class="mini-item"><span></span><span class="mini-title">None found</span><span></span></div>`;
    return;
  }
  els.projects.innerHTML = rows
    .map(({ type, item, depth, count }) => {
      const marker = `<span class="nav-icon" aria-hidden="true">${type === "project" ? projectPie(item) : icons[type]}</span>`;
      const finished = type === "project" && item.status && item.status !== "open" ? " finished" : "";
      const classes = `mini-item ${type === "area" ? "area-row" : "project-row"}${depth ? " nested" : ""}${finished}`;
      const collapsed = type === "area" && collapsedAreas.has(item.uuid);
      const attributes = type === "project" ? ` data-group="${escapeHtml(depth ? item.area : "")}"` : ` aria-expanded="${!collapsed}"`;
      // A folded area shows its open to-dos, so nothing is lost.
      const toggle =
        type === "area"
          ? `<span class="area-toggle" data-toggle-area="${escapeHtml(item.uuid)}" title="${collapsed ? "Expand" : "Collapse"}" aria-hidden="true">${collapsed ? "▸" : "▾"}</span>`
          : "";
      return `${scopeButton(type, item, classes).replace("<button ", `<button${attributes} `)}
        ${marker}
        <span class="mini-title">${escapeHtml(item.title)}</span>
        <span class="mini-end">${collapsed && count ? `<span class="count">${count}</span>` : ""}${toggle}</span>
      </button>`;
    })
    .join("");
}

// listProjects are the projects Someday and Upcoming list above their to-dos.
function listProjects() {
  if (state.scope || !["someday", "upcoming"].includes(state.view)) return [];
  return liveProjects().filter((project) => project.when === state.view).sort(byIndex);
}

// inheritedTagIDs are the tags of a to-do or project together with those of
// its project and area: in Things, items carry the tags of what holds them.
function inheritedTagIDs(item) {
  const project = item.project ? findScopeItem("project", item.project) : null;
  const areas = new Set([item.area, project?.area].filter(Boolean));
  return [
    ...(item.tagIDs || []),
    ...(project?.tagIDs || []),
    ...[...areas].flatMap((area) => findScopeItem("area", area)?.tagIDs || []),
  ];
}

// tagFilterBar shows the tags used in the list, like the tag bar in Things:
// a click filters the list by that tag, another click shows all again.
function tagFilterBar(tasks) {
  const used = new Set([...tasks, ...listProjects()].flatMap(inheritedTagIDs));
  const active = activeTagFilter();
  const tags = (state.model?.tags || []).filter((tag) => used.has(tag.uuid) || tag.uuid === active);
  if (!tags.length) return "";
  const pills = tags.map(
    (tag) =>
      `<button type="button" class="pill tag tag-filter-pill${tag.uuid === active ? " active" : ""}" data-tag-filter="${escapeHtml(tag.uuid)}" aria-pressed="${tag.uuid === active}">${escapeHtml(tag.title)}</button>`,
  );
  return `<div class="tag-filter" role="group" aria-label="Filter by tag">${pills.join("")}</div>`;
}

function toggleTagFilter(uuid) {
  state.tagFilter = activeTagFilter() === uuid ? null : { list: listKey(), uuid };
  renderTasks();
}

// projectRows lists the projects Things shows at the top of Someday and
// Upcoming.
function projectRows() {
  const tag = activeTagFilter();
  const projects = listProjects().filter((project) => !tag || inheritedTagIDs(project).includes(tag));
  if (!projects.length) return "";
  const rows = projects
    .map(
      (project) => `${scopeButton("project", project, "task-row project-link")}
          <span class="nav-icon" aria-hidden="true">${projectPie(project)}</span>
          <div class="task-title">${escapeHtml(project.title)}</div>
          <div class="task-meta">${(project.tags || []).map((title) => `<span class="pill tag">${escapeHtml(title)}</span>`).join("")}${project.count ? `<span class="pill">${project.count}</span>` : ""}</div>
        </button>`,
    )
    .join("");
  return `<section class="task-section"><h2>Projects</h2><div class="task-section-rows">${rows}</div></section>`;
}

function taskRow(task) {
  if (task.pending) {
    return `
        <article class="task-row pending" aria-busy="true" data-pending="${escapeHtml(task.uuid)}">
          <span class="check" aria-hidden="true"></span>
          <div class="task-main"><div class="task-line"><span class="task-title">${escapeHtml(task.title)}</span></div></div>
          <div class="task-meta">Adding…</div>
        </article>`;
  }
  if (state.editing === task.uuid) return taskEditor(task);
  // Notes, checklist and tags sit after the title, as in Things; where a
  // to-do belongs goes below it when the list is not grouped by it.
  const inline = [];
  if (task.reminder) inline.push(`<span class="inline-count reminder" title="Reminder">${icons.bell}${escapeHtml(task.reminder)}</span>`);
  if (task.note) inline.push(`<span class="inline-icon" title="Notes">${icons.note}</span>`);
  if (task.checklist?.length) {
    const done = task.checklist.filter((item) => item.done).length;
    inline.push(`<span class="inline-count" title="Checklist">${icons.checklist}${done}/${task.checklist.length}</span>`);
  }
  for (const tag of task.tags || []) inline.push(`<span class="pill tag">${escapeHtml(tag)}</span>`);
  const grouped = state.scope || ["today", "anytime", "someday"].includes(state.view);
  const context = grouped ? "" : task.projectTitle || task.areaTitle || "";
  const meta = [];
  if (task.deadline) meta.push(`<span class="pill deadline" title="Deadline">${icons.flag}${escapeHtml(formatDate(task.deadline))}</span>`);
  const classes = ["task-row"];
  if (state.selected.has(task.uuid)) classes.push("selected");
  if (isDone(task)) classes.push("done");
  if (task.status === "canceled") classes.push("canceled");
  const handle = canReorder() ? `<span class="drag-handle" title="Drag to reorder" aria-hidden="true">⠿</span>` : "";
  const label = isDone(task) ? "Mark open" : "Complete";
  return `
        <article class="${classes.join(" ")}" data-task="${escapeHtml(task.uuid)}">
          ${handle}
          <button class="check${isDone(task) ? " checked" : ""}${task.status === "canceled" ? " canceled" : ""}" type="button" title="${label}${isDone(task) ? "" : " (Alt+click: cancel)"}" aria-label="${label} ${escapeHtml(task.title)}" data-complete="${escapeHtml(task.uuid)}"></button>
          <div class="task-main">
            <div class="task-line"><span class="task-title">${escapeHtml(task.title)}</span>${inline.join("")}</div>
            ${context ? `<div class="task-sub">${escapeHtml(context)}</div>` : ""}
          </div>
          <div class="task-meta">${meta.join("")}</div>
        </article>`;
}

// taskEditor is the opened to-do: title, notes and checklist, edited in
// place like in Things.
function taskEditor(task) {
  const items = (task.checklist || [])
    .map(
      (item) => `
            <div class="check-item${item.done ? " done" : ""}" data-item="${escapeHtml(item.uuid || "")}">
              ${item.uuid ? `<span class="item-handle" title="Drag to reorder" aria-hidden="true">⠿</span>` : ""}
              <button class="mini-check${item.done ? " checked" : ""}" type="button" aria-label="${item.done ? "Mark open" : "Complete"} ${escapeHtml(item.title)}" data-item-toggle="${escapeHtml(item.uuid || "")}" ${item.uuid ? "" : "disabled"}></button>
              <input class="item-title" value="${escapeHtml(item.title)}" maxlength="500" aria-label="Checklist item" ${item.uuid ? "" : "readonly"} />
            </div>`,
    )
    .join("");
  return `
        <article class="task-row editing${isDone(task) ? " done" : ""}" data-task="${escapeHtml(task.uuid)}">
          <button class="check${isDone(task) ? " checked" : ""}${task.status === "canceled" ? " canceled" : ""}" type="button" aria-label="${isDone(task) ? "Mark open" : "Complete"} ${escapeHtml(task.title)}" data-complete="${escapeHtml(task.uuid)}"></button>
          <div class="task-editor">
            <input class="edit-title" value="${escapeHtml(task.title)}" maxlength="500" aria-label="Title" />
            <textarea class="edit-note" rows="1" maxlength="5000" placeholder="Notes" aria-label="Notes">${escapeHtml(task.note || "")}</textarea>
            ${task.note ? notePreview(task.note) : ""}
            <div class="checklist">${items}
              <div class="check-item new">
                <span class="mini-check placeholder" aria-hidden="true"></span>
                <input class="item-new" placeholder="${items ? "New item" : "Add checklist"}" maxlength="500" aria-label="New checklist item" />
              </div>
            </div>
            ${editorFooter(task)}
          </div>
          <div class="task-meta"></div>
        </article>`;
}

// whenOf names the list or start date a to-do is scheduled for.
function whenOf(task) {
  const views = task.views || [];
  if (views.includes("upcoming")) return "date";
  if (views.includes("today")) return task.evening ? "evening" : "today";
  if (views.includes("inbox")) return "inbox";
  if (views.includes("someday")) return "someday";
  return "anytime";
}

// editorFooter edits when, deadline and tags, like the bottom of an open
// to-do in Things.
function editorFooter(task) {
  const when = whenOf(task);
  // Like Things, a reminder rings on the start date, so it needs one.
  const dated = ["today", "evening", "date"].includes(when);
  const options = [
    ["inbox", "Inbox"],
    ["today", "Today"],
    ["evening", "This Evening"],
    ["anytime", "Anytime"],
    ["someday", "Someday"],
    ["date", "On a date…"],
  ]
    .map(([value, label]) => `<option value="${value}"${value === when ? " selected" : ""}>${label}</option>`)
    .join("");
  const tags = (task.tagIDs || []).map((id, i) => {
    const title = (task.tags || [])[i] || state.model?.tags?.find((tag) => tag.uuid === id)?.title || "";
    return `<span class="pill tag">${escapeHtml(title)}<button type="button" class="tag-remove" data-tag-remove="${escapeHtml(id)}" aria-label="Remove tag ${escapeHtml(title)}">×</button></span>`;
  });
  const tagOptions = (state.model?.tags || []).map((tag) => `<option value="${escapeHtml(tag.title)}"></option>`).join("");
  return `
            <div class="editor-footer">
              <label class="field" title="When">
                <span class="field-icon">${icons[when === "date" ? "upcoming" : when] || icons.anytime}</span>
                <select class="edit-when" aria-label="When">${options}</select>
                <input class="edit-start" type="date" aria-label="Start date" value="${escapeHtml(when === "date" ? task.scheduledDate || "" : "")}" ${when === "date" ? "" : "hidden"} />
              </label>
              <label class="field" title="${dated ? "Reminder" : "A reminder needs a date: Today, This Evening or a start date"}">
                <span class="field-icon">${icons.bell}</span>
                <input class="edit-reminder" type="time" aria-label="Reminder" value="${escapeHtml(task.reminder || "")}" ${dated ? "" : "disabled"} />
                ${task.reminder ? `<button type="button" class="field-clear" data-clear-reminder aria-label="Remove reminder">×</button>` : ""}
              </label>
              <label class="field" title="Deadline">
                <span class="field-icon deadline">${icons.flag}</span>
                <input class="edit-deadline" type="date" aria-label="Deadline" value="${escapeHtml(task.deadline || "")}" />
                ${task.deadline ? `<button type="button" class="field-clear" data-clear-deadline aria-label="Remove deadline">×</button>` : ""}
              </label>
              <div class="field tags-field">
                <span class="field-icon">${icons.tag}</span>
                ${tags.join("")}
                <input class="tag-new" list="tag-options" placeholder="Add tag" aria-label="Add tag" maxlength="200" />
                <datalist id="tag-options">${tagOptions}</datalist>
              </div>
              ${task.repeat ? "" : `<button type="button" class="field-action" data-repeat-task="${escapeHtml(task.uuid)}">Repeat…</button><button type="button" class="field-action" data-convert-task="${escapeHtml(task.uuid)}">Convert to Project…</button>`}
              <button type="button" class="field-action" data-cancel-task="${escapeHtml(task.uuid)}">${currentStatus(task.uuid) === "canceled" ? "Reopen" : "Cancel To-Do"}</button>
            </div>`;
}

// noteLinks repeats the links of a note so they stay clickable while the
// notes are edited.
function noteLinks(note) {
  const links = String(note).match(/https?:\/\/[^\s<>"']+|www\.[^\s<>"']+/gi) || [];
  return links.slice(0, 5).map((link) => renderSafeLink(link.replace(/[),.;\]]+$/g, ""))).join(" ");
}

// hasMarkdown tells notes that use the Markdown Things renders.
function hasMarkdown(note) {
  return /(^|\n)\s*(#{1,3} |[-*] )|\*\*[^*\n]+\*\*|(^|[^*])\*[^*\s][^*\n]*\*|\[[^\]\n]+\]\([^)\s]+\)|`[^`\n]+`/.test(String(note));
}

// markdownInline renders one line: links ([text](url) and bare URLs) go
// through renderSafeLink; everything else is escaped before **bold**,
// *italic*, _italic_ and `code` become tags, so no HTML from a note reaches
// the page.
function markdownInline(line) {
  const pattern = /\[([^\]\n]+)\]\(([^)\s]+)\)|https?:\/\/[^\s<>"')]+|www\.[^\s<>"')]+/gi;
  let html = "";
  let last = 0;
  const text = (value) =>
    escapeHtml(value)
      .replace(/`([^`]+)`/g, "<code>$1</code>")
      .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
      .replace(/(^|[^*\w])\*([^*\s][^*]*)\*(?!\w)/g, "$1<em>$2</em>")
      .replace(/(^|[^\w])_([^_\s][^_]*)_(?!\w)/g, "$1<em>$2</em>");
  for (const match of line.matchAll(pattern)) {
    html += text(line.slice(last, match.index));
    if (match[1]) {
      const link = renderSafeLink(match[2]);
      html += link.startsWith("<a ") ? link.replace(/>[^<]*<\/a>$/, `>${escapeHtml(match[1])}</a>`) : escapeHtml(match[0]);
    } else {
      const raw = match[0].replace(/[),.;\]]+$/g, "");
      html += renderSafeLink(raw) + escapeHtml(match[0].slice(raw.length));
    }
    last = match.index + match[0].length;
  }
  return html + text(line.slice(last));
}

// markdownHtml renders the Markdown subset Things shows in notes: headings
// (#, ##, ###), lists (- or *), bold, italic, code and links.
function markdownHtml(note) {
  const out = [];
  let list = false;
  for (const line of String(note).split("\n")) {
    const item = line.match(/^\s*[-*] (.*)$/);
    if (item) {
      if (!list) out.push("<ul>");
      list = true;
      out.push(`<li>${markdownInline(item[1])}</li>`);
      continue;
    }
    if (list) out.push("</ul>");
    list = false;
    const heading = line.match(/^\s*(#{1,3}) (.*)$/);
    if (heading) out.push(`<div class="md-h${heading[1].length}">${markdownInline(heading[2])}</div>`);
    else if (line.trim()) out.push(`<p>${markdownInline(line)}</p>`);
  }
  if (list) out.push("</ul>");
  return out.join("");
}

// notePreview shows a note's Markdown below the notes field, like Things
// renders it; plain notes only repeat their links there.
function notePreview(note) {
  return hasMarkdown(note) ? `<div class="note-markdown">${markdownHtml(note)}</div>` : `<div class="note-links">${noteLinks(note)}</div>`;
}

// wrapNoteSelection wraps the selected text of a notes field in a Markdown
// marker (** bold, * italic), as Mod+B and Mod+I do in Things.
function wrapNoteSelection(field, marker) {
  const { selectionStart: start, selectionEnd: end, value } = field;
  field.value = value.slice(0, start) + marker + value.slice(start, end) + marker + value.slice(end);
  field.setSelectionRange(start + marker.length, end + marker.length);
  field.dispatchEvent(new Event("input", { bubbles: true }));
}

function inArchive() {
  return !state.scope && archiveViews.includes(state.view);
}

// loadArchive fetches the Logbook or the Trash; limit grows with "Show more"
// (the server allows up to 2000).
async function loadArchive(view, limit = state.archive[view]?.limit || 300) {
  try {
    const result = await api(`/api/${view}?limit=${limit}`);
    state.archive[view] = { ...result, limit };
  } catch (error) {
    state.archive[view] = { items: [], total: 0, limit, error: error.message };
  }
  if (state.view === view) renderTasks();
}

// archiveMonth names the month the Logbook groups an entry under, like
// Things ("October 2026").
function archiveMonth(value) {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" }).format(new Date(value));
}

// archiveRowHtml is a Logbook or Trash entry: ticking a logged to-do reopens
// it, Restore takes an item out of the Trash.
function archiveRowHtml(item, view) {
  const context = item.projectTitle || item.areaTitle || "";
  const action =
    view === "trash"
      ? `<button type="button" class="secondary-button restore" data-restore="${escapeHtml(item.uuid)}">Restore</button>`
      : item.completedAt
        ? `<span class="archive-date">${escapeHtml(new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" }).format(new Date(item.completedAt)))}</span>`
        : "";
  const check =
    view === "logbook" && item.type === "task"
      ? `<button class="check checked${item.status === "canceled" ? " canceled" : ""}" type="button" aria-label="Mark open ${escapeHtml(item.title)}" data-reopen="${escapeHtml(item.uuid)}"></button>`
      : `<span class="nav-icon" aria-hidden="true">${item.type === "project" ? icons.project : ""}</span>`;
  return `
        <article class="task-row archived" data-archive="${escapeHtml(item.uuid)}">
          ${check}
          <div class="task-main">
            <div class="task-line"><span class="task-title">${escapeHtml(item.title)}</span></div>
            ${context ? `<div class="task-sub">${escapeHtml(context)}</div>` : ""}
          </div>
          <div class="task-meta">${action}</div>
        </article>`;
}

// renderArchive shows the Logbook (by month; ticking reopens a to-do) or the
// Trash (with Restore).
function renderArchive() {
  const data = state.archive[state.view];
  if (!data) {
    els.summary.textContent = "";
    els.tasks.innerHTML = `<div class="empty">Loading…</div>`;
    return;
  }
  const query = state.query.trim().toLowerCase();
  const items = data.items.filter((item) => !query || `${item.title} ${item.projectTitle} ${item.areaTitle}`.toLowerCase().includes(query));
  els.summary.textContent = data.total > data.items.length ? `${data.items.length} of ${data.total}` : `${items.length} items`;
  if (data.error) {
    els.tasks.innerHTML = `<div class="empty">${escapeHtml(data.error)}</div>`;
    return;
  }
  if (!items.length) {
    els.tasks.innerHTML = `<div class="empty">${state.view === "trash" ? "The Trash is empty." : "Nothing logged yet."}</div>`;
    return;
  }
  const row = (item) => archiveRowHtml(item, state.view);
  const more =
    data.total > data.items.length && (data.limit || 300) < 2000
      ? `<div class="archive-more"><button type="button" class="secondary-button" data-archive-more>Show more</button></div>`
      : "";
  if (state.view === "trash") {
    const empty = `<div class="trash-actions"><button type="button" class="secondary-button" data-empty-trash>Empty Trash</button></div>`;
    els.tasks.innerHTML = `${empty}<section class="task-section"><div class="task-section-rows">${items.map(row).join("")}</div></section>${more}`;
    return;
  }
  const groups = new Map();
  for (const item of items) {
    const month = archiveMonth(item.completedAt);
    if (!groups.has(month)) groups.set(month, []);
    groups.get(month).push(item);
  }
  els.tasks.innerHTML =
    [...groups]
      .map(([month, rows]) => `<section class="task-section"><h2>${escapeHtml(month)}</h2><div class="task-section-rows">${rows.map(row).join("")}</div></section>`)
      .join("") + more;
}

// searchLogbook asks the server for Logbook matches of the search, like
// Things' Quick Find, 250 ms after the last key.
let searchTimer = 0;
function searchLogbook() {
  clearTimeout(searchTimer);
  const query = state.query.trim();
  if (query.length < 2 || inArchive()) {
    state.search = null;
    return;
  }
  searchTimer = setTimeout(async () => {
    try {
      const result = await api(`/api/search?q=${encodeURIComponent(query)}`);
      if (state.query.trim() !== query) return;
      state.search = { query, items: result.items, total: result.total };
    } catch {
      state.search = null;
    }
    renderTasks();
  }, 250);
}

// searchSection lists the Logbook matches below the open ones.
function searchSection() {
  const search = state.search;
  if (!search?.items.length || search.query !== state.query.trim() || inArchive()) return "";
  const more = search.total > search.items.length ? `<div class="task-sub">${search.items.length} of ${search.total}</div>` : "";
  return `<section class="task-section search-logbook"><h2>Logbook</h2><div class="task-section-rows">${search.items.map((item) => archiveRowHtml(item, "logbook")).join("")}</div>${more}</section>`;
}

// archiveAction reopens a logged to-do or restores one from the Trash.
async function archiveAction(uuid, restore) {
  // Logbook matches of a search can be reopened from any list.
  const data = inArchive() ? state.archive[state.view] : state.search;
  const item = data?.items.find((entry) => entry.uuid === uuid);
  if (!item) return;
  data.items = data.items.filter((entry) => entry !== item);
  renderTasks();
  try {
    await api(restore ? "/api/restore" : "/api/reopen", { method: "POST", body: JSON.stringify(restore ? { uuids: [uuid] } : { uuid }) });
    toast(restore ? `Restored “${item.title}”` : `Reopened “${item.title}”`, "success");
    sync({ fresh: true, quiet: true });
  } catch (error) {
    data.items.unshift(item);
    renderTasks();
    toast(`Could not ${restore ? "restore" : "reopen"} “${item.title}”: ${error.message}`, "error");
  }
}

// emptyTrash deletes everything in the Trash for good after asking, like
// Things; it cannot be undone.
async function emptyTrash() {
  const data = state.archive.trash;
  if (!data?.total) return;
  const count = data.total === 1 ? "1 item" : `${data.total} items`;
  if (!window.confirm(`Empty the Trash? ${count} will be deleted for good. This cannot be undone.`)) return;
  state.archive.trash = { items: [], total: 0 };
  renderTasks();
  try {
    await api("/api/empty-trash", { method: "POST", body: "{}" });
    toast("Emptied the Trash", "success");
  } catch (error) {
    toast(`Could not empty the Trash: ${error.message}`, "error");
  }
  loadArchive("trash");
}

// canReorder: to-dos can be dragged within a section unless a search filters
// the list.
function canReorder() {
  return state.phase === "ready" && !state.query.trim() && !inArchive();
}

function sectionHeading(section) {
  if (!section.title) return "";
  if (section.kind === "heading") return `<h2 class="heading" data-heading-drop="${escapeHtml(section.heading)}" data-heading-title="${escapeHtml(section.title)}">${escapeHtml(section.title)}</h2>`;
  const icon = icons[section.kind] || "";
  return `<h2>${icon}${escapeHtml(section.title)}</h2>`;
}

function editorInUse() {
  return Boolean(state.editing && document.activeElement?.closest?.(".task-editor"));
}

function renderTasks() {
  // Do not replace the editor while it is in use, or rows while they are
  // dragged; render when that ends.
  if (editorInUse() || drag.active || els.tasks.querySelector(".rename-input")) {
    state.renderWaiting = true;
    return;
  }
  state.renderWaiting = false;
  if (inArchive()) {
    els.newHeading.hidden = true;
    els.deleteProject.hidden = true;
    els.completeProject.hidden = true;
    els.projectDetails.hidden = true;
    els.projectNote.hidden = true;
    els.viewTitle.textContent = viewTitles[state.view];
    els.viewIcon.innerHTML = icons[state.view];
    els.logCompleted.hidden = true;
    renderArchive();
    return;
  }
  const tasks = listTasks();
  els.logCompleted.hidden = !tasks.some((task) => isDone(task));
  els.viewTitle.textContent = state.scope ? state.scope.title : viewTitles[state.view];
  els.viewTitle.classList.toggle("renamable", Boolean(state.scope));
  els.viewTitle.title = state.scope ? "Click to rename" : "";
  const scopedProject = state.scope?.type === "project" ? findScopeItem("project", state.scope.id) : null;
  els.viewIcon.innerHTML = scopedProject ? projectPie(scopedProject) : icons[state.scope ? state.scope.type : state.view] || "";

  if (!state.model) {
    els.tasks.innerHTML = "";
    return;
  }

  els.summary.textContent = tasks.length === 1 ? "1 to-do" : `${tasks.length} to-dos`;

  const projectsHtml = tagFilterBar(listTasks(true)) + projectRows();
  const projectScope = state.scope?.type === "project";
  els.newHeading.hidden = !projectScope;
  els.deleteProject.hidden = !projectScope;
  els.completeProject.hidden = !projectScope || (scopedProject?.status || "open") !== "open";
  renderProjectNote();
  renderProjectDetails();
  if (!tasks.length && !(projectScope && (state.model.headings || []).some((heading) => heading.project === state.scope.id))) {
    const found = searchSection();
    els.tasks.innerHTML = projectsHtml.includes("task-section") || found ? projectsHtml + found : `${projectsHtml}<div class="empty">Nothing here.</div>`;
    return;
  }

  els.tasks.innerHTML =
    projectsHtml +
    groupTasks(tasks)
      .map(
        (section) => `
        <section class="task-section${section.evening ? " evening" : ""}" data-section-heading="${escapeHtml(section.heading || "")}"${section.evening ? ' data-evening="1"' : ""}>
          ${sectionHeading(section)}
          <div class="task-section-rows">${section.tasks.map(taskRow).join("")}</div>
        </section>`,
      )
      .join("") + searchSection();
}
function tomorrow() {
  const date = new Date();
  date.setDate(date.getDate() + 1);
  return isoDay(date);
}

// quickAddContext says where a new to-do goes: every list, project and area
// accepts one. Upcoming needs a start date.
function quickAddContext() {
  if (!state.model || inArchive()) return null;
  if (state.scope?.type === "project") {
    return { placeholder: `New To-Do in ${state.scope.title}`, payload: { when: "anytime", project: state.scope.id }, views: ["anytime"] };
  }
  if (state.scope?.type === "area") {
    return { placeholder: `New To-Do in ${state.scope.title}`, payload: { when: "anytime", area: state.scope.id }, views: ["anytime"] };
  }
  if (state.view === "upcoming") {
    return { placeholder: "New To-Do", payload: {}, views: ["upcoming"], dated: true };
  }
  const views = state.view === "today" ? ["today", "anytime"] : [state.view];
  return { placeholder: "New To-Do", payload: { when: state.view }, views };
}

function renderQuickAdd() {
  const context = quickAddContext();
  els.quickAdd.hidden = !context;
  if (!context) return;
  els.quickAdd.elements.title.placeholder = context.placeholder;
  const scheduled = els.quickAdd.elements.scheduled;
  scheduled.hidden = !context.dated;
  scheduled.min = tomorrow();
  if (context.dated && !scheduled.value) scheduled.value = tomorrow();
}

function render() {
  els.loading.hidden = state.phase !== "loading";
  els.newProjectButton.hidden = state.phase !== "ready";
  els.newAreaButton.hidden = state.phase !== "ready";
  els.tagsButton.hidden = state.phase !== "ready";
  els.emptySignin.classList.toggle("visible", state.phase === "signedOut");
  renderCounts();
  renderQuickAdd();
  renderSidebar();
  renderTasks();
}

function applyModel(model, requestedAt) {
  // The lists arrive as separate copies of the to-dos; point them at the
  // same objects, so an edit shows everywhere.
  const shared = new Map(model.tasks.map((task) => [task.uuid, task]));
  for (const view of Object.keys(model.lists || {})) {
    model.lists[view] = model.lists[view].map((task) => shared.get(task.uuid) || task);
  }
  // To-dos created here stay until a sync that started after their creation:
  // an older sync does not know them yet.
  for (const [uuid, created] of state.created) {
    if (model.tasks.some((task) => task.uuid === uuid) || requestedAt > created.at) {
      state.created.delete(uuid);
      continue;
    }
    model.tasks.unshift(created.task);
    for (const view of created.task.views || []) model.lists[view]?.unshift(created.task);
  }
  state.model = model;
  state.phase = "ready";
  state.syncedAt = model.syncedAt ? new Date(model.syncedAt) : new Date();
  state.syncError = model.syncError || "";
  // Forget local changes once the server model shows them.
  const byUUID = new Map(model.tasks.map((task) => [task.uuid, task]));
  const projectIDs = new Set((model.projects || []).map((project) => project.uuid));
  for (const [uuid, override] of state.overrides) {
    const task = byUUID.get(uuid);
    if (!task || task.status === override.status || Date.now() - override.at > 5 * 60 * 1000) state.overrides.delete(uuid);
  }
  for (const [uuid, at] of state.hidden) {
    if ((!byUUID.has(uuid) && !projectIDs.has(uuid)) || Date.now() - at > 5 * 60 * 1000) state.hidden.delete(uuid);
  }
  for (const uuid of state.selected) {
    if (!byUUID.has(uuid)) state.selected.delete(uuid);
  }
  const scoped = state.scope && (state.scope.type === "project" ? model.projects : model.areas)?.find((item) => item.uuid === state.scope.id);
  if (scoped) state.scope.title = scoped.title;
  if (state.editing && !byUUID.has(state.editing)) state.editing = "";
  // Drop created tasks once a sync that started after the create returned.
  state.pending = state.pending.filter((task) => !task.createdAt || task.createdAt > requestedAt);
  render();
}

// sync loads the model. The default answer comes from the server's cache at
// once; fresh waits for a new sync with Things Cloud. Concurrent calls share
// one request of each kind.
const inflight = {};

function sync({ fresh = false, quiet = false } = {}) {
  const key = fresh ? "fresh" : "cached";
  if (inflight[key] && fresh) {
    // A change made since that sync started needs a sync that starts after
    // it; requests in the meantime share one.
    inflight.again ||= inflight[key].then(() => {
      delete inflight.again;
      return sync({ fresh: true, quiet });
    });
    return inflight.again;
  }
  if (inflight[key]) return inflight[key];
  const requestedAt = Date.now();
  state.syncing = (state.syncing || 0) + 1;
  showSyncState();
  inflight[key] = api(fresh ? "/api/sync?fresh=1" : "/api/sync")
    .then((model) => {
      applyModel(model, requestedAt);
      if (state.syncError && !quiet) toast(`Could not reach Things Cloud: ${state.syncError}`, "error");
      if (!fresh && (model.refreshing || Date.now() - state.syncedAt > 30 * 1000)) {
        // Show the cached model now and replace it when the refresh is done.
        setTimeout(() => sync({ fresh: true, quiet: true }), 0);
      }
      return model;
    })
    .catch((error) => {
      state.syncError = error.message;
      if (!state.model) {
        state.phase = "ready";
        els.summary.textContent = error.message;
        render();
      }
      if (!quiet || !state.model) toast(error.message, "error");
    })
    .finally(() => {
      delete inflight[key];
      state.syncing -= 1;
      showSyncState();
    });
  return inflight[key];
}

function refreshIfStale() {
  if (document.visibilityState !== "visible" || state.phase !== "ready") return;
  if (!state.syncedAt || Date.now() - state.syncedAt > staleAfter) sync({ fresh: true, quiet: true });
}

function findTask(uuid) {
  return state.model?.tasks?.find((item) => item.uuid === uuid);
}

function currentStatus(uuid) {
  const task = findTask(uuid);
  if (!task) return "";
  return state.overrides.get(uuid)?.status || task.status || "open";
}

// toggleComplete ticks a to-do off (or opens it again) at once. Like in
// Things it stays in its list, greyed out, until completed items are logged.
function toggleComplete(uuid) {
  const task = findTask(uuid);
  if (!task || task.pending) return;
  const done = currentStatus(uuid) === "completed" || currentStatus(uuid) === "canceled";
  const previous = state.overrides.get(uuid);
  state.overrides.set(uuid, { status: done ? "open" : "completed", at: Date.now() });
  render();
  api(done ? "/api/reopen" : "/api/complete", { method: "POST", body: JSON.stringify({ uuid }) })
    .then(() => sync({ fresh: true, quiet: true }))
    .catch((error) => {
      if (previous) state.overrides.set(uuid, previous);
      else state.overrides.delete(uuid);
      render();
      toast(`Could not ${done ? "reopen" : "complete"} “${task.title}”: ${error.message}`, "error");
    });
}

// cancelTasks cancels to-dos at once, like Things' "Cancel" (ss 2); they stay
// in their list, crossed out, until logged. When every one of them is
// canceled already, they are opened again instead.
function cancelTasks(uuids) {
  const tasks = uuids.map(findTask).filter((task) => task && !task.pending);
  if (!tasks.length) return;
  const reopen = tasks.every((task) => currentStatus(task.uuid) === "canceled");
  const targets = reopen ? tasks : tasks.filter((task) => currentStatus(task.uuid) === "open");
  if (!targets.length) return;
  const previous = new Map(targets.map((task) => [task.uuid, state.overrides.get(task.uuid)]));
  for (const task of targets) state.overrides.set(task.uuid, { status: reopen ? "open" : "canceled", at: Date.now() });
  render();
  const undo = () => {
    for (const [uuid, before] of previous) {
      if (before) state.overrides.set(uuid, before);
      else state.overrides.delete(uuid);
    }
    render();
  };
  const request = reopen
    ? Promise.all(targets.map((task) => api("/api/reopen", { method: "POST", body: JSON.stringify({ uuid: task.uuid }) })))
    : api("/api/cancel", { method: "POST", body: JSON.stringify({ uuids: targets.map((task) => task.uuid) }) });
  return request
    .then(() => sync({ fresh: true, quiet: true }))
    .catch((error) => {
      undo();
      toast(`Could not ${reopen ? "reopen" : "cancel"}: ${error.message}`, "error");
    });
}

// logCompleted moves completed to-dos to the Logbook, like Things' "Log
// Completed".
function logCompleted() {
  const done = visible(state.model?.tasks || []).filter(isDone);
  for (const task of done) state.hidden.set(task.uuid, Date.now());
  render();
  api("/api/log-completed", { method: "POST", body: "{}" })
    .then(() => {
      toast(done.length === 1 ? "Logged 1 to-do" : `Logged ${done.length} to-dos`, "success");
      sync({ fresh: true, quiet: true });
    })
    .catch((error) => {
      for (const task of done) state.hidden.delete(task.uuid);
      render();
      toast(`Could not log completed to-dos: ${error.message}`, "error");
    });
}

// trashTasks moves to-dos to the Things Trash at once and offers Undo.
function trashTasks(uuids) {
  uuids = uuids.filter((uuid) => findTask(uuid));
  if (!uuids.length) return;
  const titles = uuids.map((uuid) => findTask(uuid).title);
  for (const uuid of uuids) state.hidden.set(uuid, Date.now());
  state.selected.clear();
  if (uuids.includes(state.editing)) state.editing = "";
  render();
  api("/api/trash", { method: "POST", body: JSON.stringify({ uuids }) })
    .then(() => {
      const label = uuids.length === 1 ? `Moved “${titles[0]}” to the Trash` : `Moved ${uuids.length} to-dos to the Trash`;
      toast(label, "info", { label: "Undo", run: () => restoreTasks(uuids) });
      sync({ fresh: true, quiet: true });
    })
    .catch((error) => {
      for (const uuid of uuids) state.hidden.delete(uuid);
      render();
      toast(`Could not delete: ${error.message}`, "error");
    });
}

function restoreTasks(uuids) {
  api("/api/restore", { method: "POST", body: JSON.stringify({ uuids }) })
    .then(async () => {
      await sync({ fresh: true, quiet: true });
      for (const uuid of uuids) state.hidden.delete(uuid);
      render();
      toast(uuids.length === 1 ? "Restored the to-do" : `Restored ${uuids.length} to-dos`, "success");
    })
    .catch((error) => toast(`Could not restore: ${error.message}`, "error"));
}

// sidebarProject is the project last clicked in the sidebar while it is still
// open and nothing else was clicked since, like a selected project in the
// Things sidebar.
function sidebarProject() {
  if (!state.sidebarProject || state.selected.size || state.editing) return "";
  return state.scope?.type === "project" && state.scope.id === state.sidebarProject ? state.sidebarProject : "";
}

// deleteProject moves a project and its to-dos to the Trash after asking,
// like Things, and offers Undo.
function deleteProject(uuid) {
  const project = liveProjects().find((item) => item.uuid === uuid);
  if (!project) return;
  const tasks = (state.model?.tasks || []).filter((task) => task.project === uuid && !state.hidden.has(task.uuid));
  const open = tasks.filter((task) => !isDone(task)).length;
  const contents = open === 1 ? " and its 1 open to-do" : open ? ` and its ${open} open to-dos` : "";
  if (!window.confirm(`Move the project “${project.title}”${contents} to the Trash?`)) return;
  const hidden = [uuid, ...tasks.map((task) => task.uuid)];
  const now = Date.now();
  for (const id of hidden) state.hidden.set(id, now);
  state.sidebarProject = "";
  if (state.scope?.id === uuid) selectView("inbox");
  else render();
  api("/api/trash", { method: "POST", body: JSON.stringify({ uuids: [uuid] }) })
    .then(() => {
      toast(`Moved “${project.title}” to the Trash`, "info", { label: "Undo", run: () => restoreProject(project, hidden) });
      sync({ fresh: true, quiet: true });
    })
    .catch((error) => {
      for (const id of hidden) state.hidden.delete(id);
      render();
      toast(`Could not delete “${project.title}”: ${error.message}`, "error");
    });
}

// finishProject completes or cancels a project like Things: when to-dos are
// still open, it asks whether to complete or cancel them too. The project
// and its to-dos stay, ticked or crossed out, until "Log Completed".
async function finishProject(uuid, mode) {
  const project = liveProjects().find((item) => item.uuid === uuid);
  if (!project || (project.status || "open") !== "open") return;
  const open = (state.model?.tasks || []).filter((task) => task.project === uuid && !isDone(task) && !state.hidden.has(task.uuid));
  let rest = mode;
  if (open.length) {
    const verb = mode === "complete" ? "Complete" : "Cancel";
    document.querySelector("#project-finish-title").textContent = `${verb} “${project.title}”`;
    document.querySelector("#project-finish-text").textContent =
      open.length === 1 ? "This project still has 1 open to-do. Complete or cancel it?" : `This project still has ${open.length} open to-dos. Complete or cancel them?`;
    els.projectFinish.returnValue = "";
    els.projectFinish.showModal();
    rest = await new Promise((resolve) => els.projectFinish.addEventListener("close", () => resolve(els.projectFinish.returnValue), { once: true }));
    if (!rest) return;
  }
  const before = { status: project.status, done: project.done };
  const tasksBefore = open.map((task) => task.status);
  project.status = mode === "complete" ? "completed" : "canceled";
  if (rest === "complete") project.done = project.total;
  for (const task of open) task.status = rest === "complete" ? "completed" : "canceled";
  render();
  try {
    await api("/api/project-complete", { method: "POST", body: JSON.stringify({ uuid, mode, rest }) });
    toast(`${mode === "complete" ? "Completed" : "Canceled"} “${project.title}”`, "success");
    sync({ fresh: true, quiet: true });
  } catch (error) {
    Object.assign(project, before);
    open.forEach((task, i) => (task.status = tasksBefore[i]));
    render();
    toast(`Could not ${mode} “${project.title}”: ${error.message}`, "error");
  }
}

// renderProjectDetails shows the project's When, deadline and tags under its
// title, like the bottom of an open to-do; an area shows its tags only.
function renderProjectDetails() {
  const project = state.scope ? findScopeItem(state.scope.type, state.scope.id) : null;
  els.projectDetails.hidden = !project;
  if (!project || els.projectDetails.contains(document.activeElement)) return;
  const area = state.scope.type === "area";
  const dated = project.when === "upcoming" || (project.scheduledDate && project.scheduledDate > isoDay(new Date()));
  const when = dated ? "date" : project.when === "someday" ? "someday" : project.scheduledDate === isoDay(new Date()) ? "today" : "anytime";
  const options = [
    ["anytime", "Anytime"],
    ["today", "Today"],
    ["someday", "Someday"],
    ["date", "On a date…"],
  ]
    .map(([value, label]) => `<option value="${value}"${value === when ? " selected" : ""}>${label}</option>`)
    .join("");
  const tags = (project.tagIDs || []).map((id, i) => {
    const title = (project.tags || [])[i] || state.model?.tags?.find((tag) => tag.uuid === id)?.title || "";
    return `<span class="pill tag">${escapeHtml(title)}<button type="button" class="tag-remove" data-project-tag-remove="${escapeHtml(id)}" aria-label="Remove tag ${escapeHtml(title)}">×</button></span>`;
  });
  const tagOptions = (state.model?.tags || []).map((tag) => `<option value="${escapeHtml(tag.title)}"></option>`).join("");
  els.projectDetails.dataset.project = project.uuid;
  const tagsField = `
    <div class="field tags-field">
      <span class="field-icon">${icons.tag}</span>
      ${tags.join("")}
      <input class="project-tag-new" list="project-tag-options" placeholder="Add tag" aria-label="Add ${area ? "area" : "project"} tag" maxlength="200" />
      <datalist id="project-tag-options">${tagOptions}</datalist>
    </div>`;
  if (area) {
    els.projectDetails.innerHTML = tagsField;
    return;
  }
  els.projectDetails.innerHTML = `
    <label class="field" title="When">
      <span class="field-icon">${icons[when === "date" ? "upcoming" : when] || icons.anytime}</span>
      <select class="project-when" aria-label="Project start">${options}</select>
      <input class="project-start" type="date" aria-label="Project start date" value="${escapeHtml(dated ? project.scheduledDate || "" : "")}" ${dated ? "" : "hidden"} />
    </label>
    <label class="field" title="Deadline">
      <span class="field-icon deadline">${icons.flag}</span>
      <input class="project-deadline" type="date" aria-label="Project deadline" value="${escapeHtml(project.deadline || "")}" />
    </label>
    ${tagsField}`;
}

// headerItem is the project or area uuid shown in the list header.
function headerItem(uuid) {
  return findScopeItem("project", uuid) || findScopeItem("area", uuid);
}

// saveProjectDetail sends one change of the project (or area) header and
// shows it at once; on failure the old value comes back.
async function saveProjectDetail(uuid, path, body, apply) {
  const project = headerItem(uuid);
  if (!project) return;
  const before = { when: project.when, scheduledDate: project.scheduledDate, deadline: project.deadline, tags: project.tags, tagIDs: project.tagIDs };
  apply(project);
  renderSidebar();
  renderTasks();
  try {
    await api(path, { method: "POST", body: JSON.stringify(body) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    Object.assign(project, before);
    renderSidebar();
    renderTasks();
    toast(`Could not change “${project.title}”: ${error.message}`, "error");
  }
}

function scheduleProject(uuid, change) {
  const today = isoDay(new Date());
  saveProjectDetail(uuid, "/api/schedule", { uuids: [uuid], ...change }, (project) => {
    if (change.startDate) {
      project.when = change.startDate > today ? "upcoming" : "anytime";
      project.scheduledDate = change.startDate;
    } else {
      project.when = change.when === "someday" ? "someday" : "anytime";
      project.scheduledDate = change.when === "today" ? today : "";
    }
  });
}

async function setProjectTags(uuid, tagIDs) {
  saveProjectDetail(uuid, "/api/tags", { uuid, tags: tagIDs }, (project) => {
    project.tagIDs = tagIDs;
    project.tags = tagIDs.map((id) => state.model.tags.find((tag) => tag.uuid === id)?.title || "");
  });
}

async function addProjectTag(uuid, name) {
  const project = headerItem(uuid);
  if (!project || !name) return;
  let tag = state.model.tags.find((item) => item.title.toLowerCase() === name.toLowerCase());
  if (!tag) {
    try {
      const created = await api("/api/tag-create", { method: "POST", body: JSON.stringify({ title: name }) });
      tag = { uuid: created.uuid, title: created.title };
      state.model.tags.push(tag);
    } catch (error) {
      toast(`Could not create the tag “${name}”: ${error.message}`, "error");
      return;
    }
  }
  if (!(project.tagIDs || []).includes(tag.uuid)) setProjectTags(uuid, [...(project.tagIDs || []), tag.uuid]);
}

function restoreProject(project, hidden) {
  api("/api/restore", { method: "POST", body: JSON.stringify({ uuids: [project.uuid] }) })
    .then(async () => {
      await sync({ fresh: true, quiet: true });
      for (const id of hidden) state.hidden.delete(id);
      render();
      toast(`Restored “${project.title}”`, "success");
    })
    .catch((error) => toast(`Could not restore “${project.title}”: ${error.message}`, "error"));
}

// deleteArea deletes an area after asking, like current Things: its projects
// and to-dos move to the Trash, then the area is gone. Undo brings the
// projects and to-dos back without the area.
async function deleteArea(uuid) {
  const area = findScopeItem("area", uuid);
  if (!area || isTemp(uuid)) return;
  const projects = liveProjects().filter((project) => project.area === uuid);
  const tasks = (state.model?.tasks || []).filter((task) => task.area === uuid && !state.hidden.has(task.uuid));
  const count = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
  const parts = [projects.length && count(projects.length, "project"), tasks.length && count(tasks.length, "to-do")].filter(Boolean);
  const contents = parts.length ? ` Its ${parts.join(" and ")} move to the Trash.` : "";
  if (!window.confirm(`Delete the area “${area.title}”?${contents}`)) return;
  const areas = state.model.areas;
  const hidden = [...projects, ...tasks].map((item) => item.uuid);
  const now = Date.now();
  for (const id of hidden) state.hidden.set(id, now);
  state.model.areas = areas.filter((item) => item !== area);
  state.sidebarProject = "";
  if (state.scope?.id === uuid || projects.some((project) => project.uuid === state.scope?.id)) selectView("inbox");
  else render();
  try {
    const result = await api("/api/area-delete", { method: "POST", body: JSON.stringify({ uuid }) });
    const trashed = result.trashed || [];
    toast(`Deleted the area “${area.title}”`, "info", trashed.length ? { label: "Undo", run: () => restoreAreaContents(area, trashed, hidden) } : null);
    sync({ fresh: true, quiet: true });
  } catch (error) {
    state.model.areas = areas;
    for (const id of hidden) state.hidden.delete(id);
    render();
    toast(`Could not delete “${area.title}”: ${error.message}`, "error");
  }
}

// restoreAreaContents brings a deleted area's projects and to-dos back from
// the Trash; the area itself stays deleted, as in Things.
function restoreAreaContents(area, trashed, hidden) {
  api("/api/restore", { method: "POST", body: JSON.stringify({ uuids: trashed }) })
    .then(async () => {
      await sync({ fresh: true, quiet: true });
      for (const id of hidden) state.hidden.delete(id);
      render();
      toast(`Restored the contents of “${area.title}”; the area stays deleted`, "success");
    })
    .catch((error) => toast(`Could not restore: ${error.message}`, "error"));
}

// projectMenu offers Rename and, for projects, Complete, Cancel and Delete
// Project, for areas Delete Area, on a right click in the sidebar.
function projectMenu(event) {
  const row = event.target.closest(".mini-item[data-scope-id]");
  if (!row) return;
  event.preventDefault();
  closeProjectMenu();
  const project = row.dataset.scopeType === "project";
  const menu = document.createElement("div");
  menu.className = "context-menu";
  menu.setAttribute("role", "menu");
  const open = project && (findScopeItem("project", row.dataset.scopeId)?.status || "open") === "open";
  menu.innerHTML = `<button type="button" role="menuitem" data-action="rename">Rename…</button>${
    open
      ? `<button type="button" role="menuitem" data-action="complete">Complete Project…</button><button type="button" role="menuitem" data-action="cancel">Cancel Project…</button>`
      : ""
  }${
    project
      ? `<button type="button" role="menuitem" class="danger" data-action="delete">Delete Project…</button>`
      : `<button type="button" role="menuitem" class="danger" data-action="delete-area">Delete Area…</button>`
  }`;
  menu.style.left = `${Math.min(event.clientX, window.innerWidth - 180)}px`;
  menu.style.top = `${Math.min(event.clientY, window.innerHeight - 150)}px`;
  menu.addEventListener("click", (click) => {
    const action = click.target.closest("[data-action]")?.dataset.action;
    if (!action) return;
    closeProjectMenu();
    if (action === "delete") deleteProject(row.dataset.scopeId);
    else if (action === "delete-area") deleteArea(row.dataset.scopeId);
    else if (action === "complete" || action === "cancel") finishProject(row.dataset.scopeId, action);
    else renameInSidebar(row.dataset.scopeType, row.dataset.scopeId);
  });
  document.body.append(menu);
  menu.querySelector("button").focus();
}

// findScopeItem returns the project or area uuid of the model.
function findScopeItem(type, uuid) {
  return (type === "project" ? state.model?.projects : state.model?.areas)?.find((item) => item.uuid === uuid);
}

// renameItem saves a new project or area name, showing it at once.
async function renameItem(type, uuid, title) {
  const item = findScopeItem(type, uuid);
  title = title.trim();
  if (!item || !title || title === item.title) return;
  const before = item.title;
  const apply = (value) => {
    item.title = value;
    const key = type === "project" ? "project" : "area";
    for (const task of state.model?.tasks || []) if (task[key] === uuid) task[`${key}Title`] = value;
    if (state.scope?.id === uuid) state.scope.title = value;
    render();
  };
  apply(title);
  try {
    if (type === "project") await api("/api/update", { method: "POST", body: JSON.stringify({ uuid, title }) });
    else await api("/api/area-rename", { method: "POST", body: JSON.stringify({ uuid, title }) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    apply(before);
    toast(`Could not rename “${before}”: ${error.message}`, "error");
  }
}

// inlineRename swaps element's text for an input; Enter or leaving it saves,
// Escape cancels.
function inlineRename(element, value, save, done = () => {}) {
  const input = document.createElement("input");
  input.className = "rename-input";
  input.value = value;
  input.maxLength = 500;
  input.setAttribute("aria-label", "Name");
  element.replaceChildren(input);
  input.focus();
  input.select();
  let finished = false;
  const finish = (keep) => {
    if (finished) return;
    finished = true;
    input.remove();
    element.textContent = value;
    done();
    if (keep) save(input.value);
  };
  input.addEventListener("keydown", (event) => {
    event.stopPropagation();
    if (event.key === "Enter") finish(true);
    else if (event.key === "Escape") finish(false);
  });
  input.addEventListener("blur", () => finish(true));
  // Clicks and drags inside the field must not open or move the row.
  for (const type of ["click", "pointerdown", "dblclick"]) input.addEventListener(type, (event) => event.stopPropagation());
}

function renameInSidebar(type, uuid) {
  const row = els.projects.querySelector(`.mini-item[data-scope-type="${type}"][data-scope-id="${CSS.escape(uuid)}"]`);
  const item = findScopeItem(type, uuid);
  if (!row || !item) return;
  inlineRename(row.querySelector(".mini-title"), item.title, (title) => renameItem(type, uuid, title), () => {
    if (state.sidebarWaiting) renderSidebar();
  });
}

function renameInHeader() {
  if (!state.scope || els.viewTitle.querySelector("input")) return;
  const { type, id } = state.scope;
  const item = findScopeItem(type, id);
  if (!item) return;
  inlineRename(els.viewTitle, item.title, (title) => renameItem(type, id, title));
}

// renderProjectNote shows the open project's notes, editable in place like
// in Things; it is not replaced while it is being edited.
function renderProjectNote() {
  const project = state.scope?.type === "project" ? findScopeItem("project", state.scope.id) : null;
  els.projectNote.hidden = !project;
  if (!project || document.activeElement === els.projectNote) return;
  els.projectNote.dataset.project = project.uuid;
  els.projectNote.value = project.note || "";
  growNote(els.projectNote);
}

function growNote(textarea) {
  textarea.style.height = "auto";
  textarea.style.height = `${textarea.scrollHeight}px`;
}

async function saveProjectNote() {
  const uuid = els.projectNote.dataset.project;
  const project = findScopeItem("project", uuid);
  const note = els.projectNote.value.replace(/\s+$/, "");
  if (!project || note === (project.note || "")) return;
  const before = project.note || "";
  project.note = note;
  try {
    await api("/api/update", { method: "POST", body: JSON.stringify({ uuid, note }) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    project.note = before;
    renderProjectNote();
    toast(`Could not save the notes of “${project.title}”: ${error.message}`, "error");
  }
}

function closeProjectMenu() {
  document.querySelector(".context-menu")?.remove();
}

// renameHeading edits a heading's name in place and saves it.
function renameHeading(uuid) {
  const element = els.tasks.querySelector(`h2.heading[data-heading-drop="${CSS.escape(uuid)}"]`);
  const heading = state.model?.headings?.find((item) => item.uuid === uuid);
  if (!element || !heading || isTemp(uuid)) return;
  inlineRename(element, heading.title, async (value) => {
    if (state.renderWaiting) setTimeout(renderTasks, 0);
    const title = value.trim();
    if (!title || title === heading.title) return;
    const before = heading.title;
    const apply = (name) => {
      heading.title = name;
      for (const task of state.model.tasks) if (task.heading === uuid) task.headingTitle = name;
      renderTasks();
    };
    apply(title);
    try {
      await api("/api/update", { method: "POST", body: JSON.stringify({ uuid, title }) });
      sync({ fresh: true, quiet: true });
    } catch (error) {
      apply(before);
      toast(`Could not rename the heading: ${error.message}`, "error");
    }
  });
}

// deleteHeading removes a heading after asking; like Things, its to-dos stay
// in the project.
async function deleteHeading(uuid) {
  const heading = state.model?.headings?.find((item) => item.uuid === uuid);
  if (!heading || isTemp(uuid)) return;
  if (!window.confirm(`Delete the heading “${heading.title}”? Its to-dos stay in the project.`)) return;
  const headings = state.model.headings;
  const under = state.model.tasks.filter((task) => task.heading === uuid);
  state.model.headings = headings.filter((item) => item !== heading);
  for (const task of under) Object.assign(task, { heading: "", headingTitle: "" });
  renderTasks();
  try {
    await api("/api/heading-delete", { method: "POST", body: JSON.stringify({ uuid }) });
    toast(`Deleted the heading “${heading.title}”`, "success");
    sync({ fresh: true, quiet: true });
  } catch (error) {
    state.model.headings = headings;
    for (const task of under) Object.assign(task, { heading: uuid, headingTitle: heading.title });
    renderTasks();
    toast(`Could not delete the heading: ${error.message}`, "error");
  }
}

// headingMenu offers Rename and Delete Heading on a right click on a heading.
function headingMenu(event) {
  const element = event.target.closest("h2.heading[data-heading-drop]");
  if (!element) return;
  event.preventDefault();
  closeProjectMenu();
  const uuid = element.dataset.headingDrop;
  const menu = document.createElement("div");
  menu.className = "context-menu";
  menu.setAttribute("role", "menu");
  menu.innerHTML = `<button type="button" role="menuitem" data-action="rename">Rename…</button><button type="button" role="menuitem" class="danger" data-action="delete">Delete Heading…</button>`;
  menu.style.left = `${Math.min(event.clientX, window.innerWidth - 180)}px`;
  menu.style.top = `${Math.min(event.clientY, window.innerHeight - 80)}px`;
  menu.addEventListener("click", (click) => {
    const action = click.target.closest("[data-action]")?.dataset.action;
    if (!action) return;
    closeProjectMenu();
    if (action === "delete") deleteHeading(uuid);
    else renameHeading(uuid);
  });
  document.body.append(menu);
  menu.querySelector("button").focus();
}

let pendingId = 0;

// createTask shows the new task at once and keeps the form ready for the next
// one.
function createTask(form) {
  const context = quickAddContext();
  if (!context) return;
  const data = new FormData(form);
  const title = String(data.get("title") || "").trim();
  const note = String(data.get("note") || "").trim();
  if (!title) return;
  const payload = { title, note, ...context.payload };
  if (context.dated) payload.scheduled = form.elements.scheduled.value || tomorrow();
  // Space with a selection: the new to-do goes right below it, as in Things.
  const insertAfter = state.insertAfter || "";
  state.insertAfter = "";
  if (insertAfter) payload.placed = true;
  const placeholder = {
    uuid: `pending-${++pendingId}`,
    pending: true,
    title,
    note,
    scheduledDate: payload.scheduled || "",
    views: context.views,
    project: payload.project || "",
    area: payload.area || "",
    tags: [],
  };
  state.pending.push(placeholder);
  form.elements.title.value = "";
  form.elements.note.value = "";
  form.elements.title.focus();
  render();
  api("/api/create", { method: "POST", body: JSON.stringify(payload) })
    .then((result) => {
      if (result.task && state.model && !findTask(result.task.uuid)) {
        // Show the real to-do at once, so it can be opened and edited.
        state.created.set(result.task.uuid, { task: result.task, at: Date.now() });
        state.model.tasks.unshift(result.task);
        for (const view of result.task.views || []) {
          if (state.model.lists[view]) state.model.lists[view].unshift(result.task);
        }
        state.pending = state.pending.filter((item) => item !== placeholder);
        if (state.openWhenReady === placeholder.uuid) {
          state.openWhenReady = "";
          render();
          openEditor(result.task.uuid);
        } else {
          render();
        }
        const anchorRow = insertAfter && els.tasks.querySelector(`.task-row[data-task="${CSS.escape(insertAfter)}"]`);
        if (anchorRow) {
          const order = [...anchorRow.parentElement.querySelectorAll(".task-row[data-task]")].map((row) => row.dataset.task).filter((uuid) => uuid !== result.task.uuid);
          order.splice(order.indexOf(insertAfter) + 1, 0, result.task.uuid);
          reorderTask(result.task.uuid, order);
        }
      } else {
        placeholder.createdAt = Date.now();
      }
      sync({ fresh: true, quiet: true });
    })
    .catch((error) => {
      state.pending = state.pending.filter((item) => item !== placeholder);
      if (!form.elements.title.value) {
        form.elements.title.value = title;
        form.elements.note.value = note;
      }
      render();
      toast(`Could not add “${title}”: ${error.message}`, "error");
    });
}

// listForOrder is the array whose order the current list shows.
function listForOrder() {
  if (!state.model) return null;
  return state.scope ? state.model.tasks : state.model.lists[state.view];
}

// Editing in place. Changes show at once and are saved in the background;
// on failure the old value comes back.

async function saveTask(uuid, fields) {
  const task = findTask(uuid);
  if (!task) return;
  const before = { title: task.title, note: task.note };
  Object.assign(task, fields);
  try {
    await api("/api/update", { method: "POST", body: JSON.stringify({ uuid, ...fields }) });
  } catch (error) {
    Object.assign(task, before);
    renderTasks();
    toast(`Could not save “${before.title}”: ${error.message}`, "error");
  }
}

async function checklistAction(taskUUID, action, item = {}) {
  const task = findTask(taskUUID);
  if (!task) return;
  const before = (task.checklist || []).map((entry) => ({ ...entry }));
  const items = (task.checklist || []).map((entry) => ({ ...entry }));
  const at = items.findIndex((entry) => entry.uuid && entry.uuid === item.uuid);
  let added = null;
  if (action === "add") {
    added = { uuid: "", title: item.title, done: false };
    items.push(added);
  } else if (at >= 0 && action === "edit") {
    items[at].title = item.title;
  } else if (at >= 0 && action === "complete") {
    items[at].done = item.done;
  } else if (at >= 0 && action === "delete") {
    items.splice(at, 1);
  } else if (action === "reorder") {
    items.sort((a, b) => item.order.indexOf(a.uuid) - item.order.indexOf(b.uuid));
  }
  task.checklist = items;
  refreshEditorChecklist(taskUUID, action === "add");
  try {
    const result = await api("/api/checklist", {
      method: "POST",
      body: JSON.stringify({ action, task: taskUUID, ...item }),
    });
    if (added) {
      added.uuid = result.uuid || "";
      refreshEditorChecklist(taskUUID, false);
    }
  } catch (error) {
    task.checklist = before;
    refreshEditorChecklist(taskUUID, false);
    toast(`Could not change the checklist: ${error.message}`, "error");
  }
}

// refreshEditorChecklist redraws only the checklist of the open editor, so
// the title and notes keep focus and caret.
function refreshEditorChecklist(taskUUID, focusNew) {
  const row = els.tasks.querySelector(`.task-row.editing[data-task="${CSS.escape(taskUUID)}"]`);
  const task = findTask(taskUUID);
  if (!row || !task) return;
  const draft = row.querySelector(".item-new")?.value || "";
  const holder = document.createElement("div");
  holder.innerHTML = taskEditor(task);
  row.querySelector(".checklist").replaceWith(holder.querySelector(".checklist"));
  const input = row.querySelector(".item-new");
  if (focusNew) input.focus();
  else input.value = draft;
}

function autosize(textarea) {
  textarea.style.height = "auto";
  textarea.style.height = `${textarea.scrollHeight}px`;
}

function openEditor(uuid) {
  const task = findTask(uuid);
  if (!task || task.pending) return;
  closeEditor();
  state.editing = uuid;
  state.selected = new Set([uuid]);
  state.anchor = uuid;
  renderTasks();
  const row = els.tasks.querySelector(".task-row.editing");
  const note = row?.querySelector(".edit-note");
  if (note) autosize(note);
  row?.querySelector(".edit-title")?.focus();
}

// closeEditor saves what changed and shows the list again.
function closeEditor() {
  if (!state.editing) return;
  const row = els.tasks.querySelector(".task-row.editing");
  const uuid = state.editing;
  const task = findTask(uuid);
  if (row && task) {
    const title = row.querySelector(".edit-title").value.trim();
    const note = row.querySelector(".edit-note").value.replace(/\s+$/, "");
    const changes = {};
    if (title && title !== task.title) changes.title = title;
    if (note !== (task.note || "")) changes.note = note;
    if (Object.keys(changes).length) saveTask(uuid, changes);
    clearTimeout(noteTimer);
  }
  state.editing = "";
  document.activeElement?.blur?.();
  renderTasks();
}

let noteTimer = 0;

// visibleRows lists the to-do rows in screen order.
function visibleRows() {
  return [...els.tasks.querySelectorAll(".task-row[data-task]")].map((row) => row.dataset.task);
}

function select(uuid, { extend = false, toggle = false } = {}) {
  if (toggle) {
    if (state.selected.has(uuid)) state.selected.delete(uuid);
    else state.selected.add(uuid);
    state.anchor = uuid;
  } else if (extend && state.anchor) {
    const rows = visibleRows();
    const [a, b] = [rows.indexOf(state.anchor), rows.indexOf(uuid)].sort((x, y) => x - y);
    state.selected = new Set(rows.slice(a, b + 1));
  } else {
    state.selected = new Set([uuid]);
    state.anchor = uuid;
  }
  renderSelection();
}

// renderSelection marks the selected rows without redrawing the list, so a
// double click still reaches the same row.
function renderSelection() {
  for (const row of els.tasks.querySelectorAll(".task-row[data-task]")) {
    row.classList.toggle("selected", state.selected.has(row.dataset.task));
  }
}

function moveSelection(step, extend) {
  const rows = visibleRows();
  if (!rows.length) return;
  const current = rows.indexOf([...state.selected].pop() || "");
  const next = rows[Math.max(0, Math.min(rows.length - 1, current < 0 ? (step > 0 ? 0 : rows.length - 1) : current + step))];
  if (extend) {
    const anchor = state.anchor || next;
    state.anchor = anchor;
    select(next, { extend: true });
    state.anchor = anchor;
  } else {
    select(next);
  }
  els.tasks.querySelector(`.task-row[data-task="${CSS.escape(next)}"]`)?.scrollIntoView({ block: "nearest" });
}

// moveSelected moves the selected to-do one place up or down in its section.
function moveSelected(step) {
  if (state.selected.size !== 1 || !canReorder()) return;
  const uuid = [...state.selected][0];
  const row = els.tasks.querySelector(`.task-row[data-task="${CSS.escape(uuid)}"]`);
  if (!row) return;
  const order = [...row.parentElement.querySelectorAll(".task-row[data-task]")].map((item) => item.dataset.task);
  const at = order.indexOf(uuid);
  const to = at + step;
  if (to < 0 || to >= order.length) return;
  order.splice(at, 1);
  order.splice(to, 0, uuid);
  reorderTask(uuid, order);
}

// scheduleTasks sets when to-dos are due: a list or a start date. The
// to-dos move at once; on failure they go back.
async function scheduleTasks(uuids, change) {
  const tasks = uuids.map(findTask).filter(Boolean);
  if (!tasks.length) return;
  const before = tasks.map((task) => ({ views: task.views, scheduledDate: task.scheduledDate, todayDate: task.todayDate, evening: task.evening }));
  const lists = Object.fromEntries(Object.entries(state.model.lists).map(([view, rows]) => [view, [...rows]]));
  const today = isoDay(new Date());
  for (const task of tasks) {
    if (change.startDate) {
      task.views = change.startDate > today ? ["upcoming"] : ["today", "anytime"];
      task.scheduledDate = task.todayDate = change.startDate;
    } else {
      task.views = { today: ["today", "anytime"], evening: ["today", "anytime"], anytime: ["anytime"], someday: task.project ? [] : ["someday"], inbox: ["inbox"] }[change.when];
      task.scheduledDate = task.todayDate = ["today", "evening"].includes(change.when) ? today : "";
    }
    // This Evening is Today with Things' sb flag; every other schedule ends it.
    task.evening = change.when === "evening";
  }
  relist(tasks);
  renderCounts();
  if (state.view === "today") renderTasks();
  try {
    await api("/api/schedule", { method: "POST", body: JSON.stringify({ uuids: tasks.map((task) => task.uuid), ...change }) });
    sync({ fresh: true, quiet: true });
    return true;
  } catch (error) {
    tasks.forEach((task, i) => Object.assign(task, before[i]));
    state.model.lists = lists;
    render();
    toast(`Could not reschedule: ${error.message}`, "error");
    return false;
  }
}

// inEvening tells whether a row sits in Today's This Evening section.
function inEvening(row) {
  return row?.closest(".task-section")?.dataset.evening === "1";
}

// relist puts to-dos into the lists their views name.
function relist(tasks) {
  const moved = new Set(tasks.map((task) => task.uuid));
  for (const [view, rows] of Object.entries(state.model.lists)) {
    state.model.lists[view] = rows.filter((row) => !moved.has(row.uuid) || row.views.includes(view));
    for (const task of tasks) {
      if (task.views.includes(view) && !state.model.lists[view].includes(task)) state.model.lists[view].unshift(task);
    }
  }
}

async function saveDeadline(uuid, deadline) {
  const task = findTask(uuid);
  if (!task) return;
  const before = task.deadline;
  task.deadline = deadline;
  refreshEditorFooter(uuid);
  try {
    await api("/api/deadline", { method: "POST", body: JSON.stringify({ uuid, deadline }) });
  } catch (error) {
    task.deadline = before;
    refreshEditorFooter(uuid);
    toast(`Could not change the deadline: ${error.message}`, "error");
  }
}

// openRepeat makes an open to-do repeat, like Things' "Repeat…": the dialog
// asks for the rule and the first day; a template takes the to-do's place
// and the Things apps create the to-dos. Undo brings the original back and
// trashes the template.
async function openRepeat(uuid) {
  const task = findTask(uuid);
  if (!task || isTemp(uuid)) return;
  if (isDone(task) || task.repeat) {
    toast(task.repeat ? `“${task.title}” already repeats` : "Only open to-dos can repeat", "error");
    return;
  }
  const form = els.repeatDialog.querySelector("form");
  form.reset();
  const today = isoDay(new Date());
  form.elements.start.min = today;
  form.elements.start.value = task.scheduledDate > today ? task.scheduledDate : today;
  document.querySelector("#repeat-title").textContent = `Repeat “${task.title}”`;
  document.querySelector("#repeat-month").hidden = true;
  els.repeatDialog.returnValue = "";
  els.repeatDialog.showModal();
  const choice = await new Promise((resolve) => els.repeatDialog.addEventListener("close", () => resolve(els.repeatDialog.returnValue), { once: true }));
  if (choice !== "repeat") return;
  const rule = {
    type: form.elements.type.value,
    unit: form.elements.unit.value,
    every: Number(form.elements.every.value),
    start: form.elements.start.value,
  };
  if (rule.unit === "month") rule.month = form.elements.month.value;
  state.hidden.set(uuid, Date.now());
  if (state.editing === uuid) state.editing = "";
  state.selected.delete(uuid);
  render();
  try {
    const result = await api("/api/repeat", { method: "POST", body: JSON.stringify({ uuid, ...rule }) });
    toast(`“${task.title}” now repeats`, "success", { label: "Undo", run: () => undoRepeat(task, result.uuid) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    state.hidden.delete(uuid);
    render();
    toast(`Could not make “${task.title}” repeat: ${error.message}`, "error");
  }
}

function undoRepeat(task, template) {
  api("/api/restore", { method: "POST", body: JSON.stringify({ uuids: [task.uuid] }) })
    .then(() => (template ? api("/api/trash", { method: "POST", body: JSON.stringify({ uuids: [template] }) }) : null))
    .then(async () => {
      await sync({ fresh: true, quiet: true });
      state.hidden.delete(task.uuid);
      render();
      toast(`“${task.title}” no longer repeats`, "success");
    })
    .catch((error) => toast(`Could not undo: ${error.message}`, "error"));
}

// openTags lists the tags to rename or delete them, like Things' tag window.
function openTags() {
  renderTagsList();
  els.tagsDialog.showModal();
}

function renderTagsList() {
  const list = document.querySelector("#tags-list");
  const tags = state.model?.tags || [];
  list.innerHTML = tags.length
    ? tags
        .map(
          (tag) => `<li class="tags-row">
            <input type="text" maxlength="200" value="${escapeHtml(tag.title)}" aria-label="Name of ${escapeHtml(tag.title)}" data-tag-rename="${escapeHtml(tag.uuid)}" />
            <button type="button" class="secondary-button" data-tag-delete="${escapeHtml(tag.uuid)}" aria-label="Delete ${escapeHtml(tag.title)}">Delete</button>
          </li>`,
        )
        .join("")
    : `<li class="empty">No tags yet.</li>`;
}

async function renameTag(uuid, title) {
  const tag = state.model?.tags?.find((item) => item.uuid === uuid);
  title = title.trim();
  if (!tag || !title || title === tag.title) {
    renderTagsList();
    return;
  }
  const before = tag.title;
  const apply = (value) => {
    tag.title = value;
    for (const item of [...(state.model.tasks || []), ...(state.model.projects || []), ...(state.model.areas || [])]) {
      const at = (item.tagIDs || []).indexOf(uuid);
      if (at >= 0 && item.tags) item.tags[at] = value;
    }
    render();
  };
  apply(title);
  try {
    await api("/api/tag-rename", { method: "POST", body: JSON.stringify({ uuid, title }) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    apply(before);
    renderTagsList();
    toast(`Could not rename “${before}”: ${error.message}`, "error");
  }
}

async function deleteTag(uuid) {
  const tag = state.model?.tags?.find((item) => item.uuid === uuid);
  if (!tag) return;
  const used = [...(state.model.tasks || []), ...(state.model.projects || []), ...(state.model.areas || [])].filter((item) => (item.tagIDs || []).includes(uuid)).length;
  const where = used === 1 ? " It is removed from 1 item." : used ? ` It is removed from ${used} items.` : "";
  if (!window.confirm(`Delete the tag “${tag.title}”?${where}`)) return;
  const model = state.model;
  const before = { tags: model.tags, rows: [...(model.tasks || []), ...(model.projects || []), ...(model.areas || [])].map((item) => [item, item.tagIDs, item.tags]) };
  model.tags = model.tags.filter((item) => item !== tag);
  for (const [item, ids, titles] of before.rows) {
    const at = (ids || []).indexOf(uuid);
    if (at < 0) continue;
    item.tagIDs = ids.filter((id) => id !== uuid);
    item.tags = (titles || []).filter((_, i) => i !== at);
  }
  if (activeTagFilter() === uuid) state.tagFilter = null;
  renderTagsList();
  render();
  try {
    await api("/api/tag-delete", { method: "POST", body: JSON.stringify({ uuid }) });
    toast(`Deleted the tag “${tag.title}”`, "success");
    sync({ fresh: true, quiet: true });
  } catch (error) {
    model.tags = before.tags;
    for (const [item, ids, titles] of before.rows) Object.assign(item, { tagIDs: ids, tags: titles });
    renderTagsList();
    render();
    toast(`Could not delete “${tag.title}”: ${error.message}`, "error");
  }
}

// openProject shows a project, as a click on it in the sidebar does.
function openProject(project) {
  closeEditor();
  state.selected.clear();
  state.sidebarProject = "";
  rememberPlace();
  state.scope = { type: "project", id: project.uuid, title: project.title };
  document.querySelectorAll(".nav-item").forEach((nav) => nav.classList.remove("active"));
  render();
}

// convertToProject turns an open to-do into a project after asking, like
// Things: its checklist items become the project's to-dos. The new project
// opens; Undo brings the to-do back, the project stays.
async function convertToProject(uuid) {
  const task = findTask(uuid);
  if (!task || isTemp(uuid) || isDone(task) || task.repeat) return;
  const items = (task.checklist || []).length;
  const contents = items === 1 ? " Its checklist item becomes a to-do." : items ? ` Its ${items} checklist items become to-dos.` : "";
  if (!window.confirm(`Convert “${task.title}” to a project?${contents}`)) return;
  state.hidden.set(uuid, Date.now());
  if (state.editing === uuid) state.editing = "";
  state.selected.delete(uuid);
  render();
  try {
    const result = await api("/api/convert-project", { method: "POST", body: JSON.stringify({ uuid }) });
    await sync({ fresh: true, quiet: true });
    const project = findScopeItem("project", result.uuid);
    if (project) openProject(project);
    toast(`Converted “${task.title}” to a project`, "success", { label: "Undo", run: () => restoreConverted(task) });
  } catch (error) {
    state.hidden.delete(uuid);
    render();
    toast(error.message, "error");
    sync({ fresh: true, quiet: true });
  }
}

function restoreConverted(task) {
  api("/api/restore", { method: "POST", body: JSON.stringify({ uuids: [task.uuid] }) })
    .then(async () => {
      await sync({ fresh: true, quiet: true });
      state.hidden.delete(task.uuid);
      render();
      toast(`Restored “${task.title}”; the project stays`, "success");
    })
    .catch((error) => toast(`Could not restore “${task.title}”: ${error.message}`, "error"));
}

// saveReminder sets ("HH:MM") or removes ("") a to-do's reminder.
async function saveReminder(uuid, time) {
  const task = findTask(uuid);
  if (!task || (task.reminder || "") === time) return;
  const before = task.reminder || "";
  task.reminder = time;
  refreshEditorFooter(uuid);
  try {
    await api("/api/reminder", { method: "POST", body: JSON.stringify({ uuid, time }) });
  } catch (error) {
    task.reminder = before;
    refreshEditorFooter(uuid);
    toast(`Could not change the reminder: ${error.message}`, "error");
  }
}

async function saveTags(uuid, tagIDs) {
  const task = findTask(uuid);
  if (!task) return;
  const before = { tagIDs: task.tagIDs, tags: task.tags };
  task.tagIDs = tagIDs;
  task.tags = tagIDs.map((id) => state.model.tags.find((tag) => tag.uuid === id)?.title || "");
  refreshEditorFooter(uuid);
  try {
    await api("/api/tags", { method: "POST", body: JSON.stringify({ uuid, tags: tagIDs }) });
  } catch (error) {
    Object.assign(task, before);
    refreshEditorFooter(uuid);
    toast(`Could not change the tags: ${error.message}`, "error");
  }
}

// addTag adds a tag by name, creating it when it does not exist yet.
async function addTag(uuid, name) {
  const task = findTask(uuid);
  if (!task || !name) return;
  let tag = state.model.tags.find((item) => item.title.toLowerCase() === name.toLowerCase());
  if (!tag) {
    try {
      const created = await api("/api/tag-create", { method: "POST", body: JSON.stringify({ title: name }) });
      tag = { uuid: created.uuid, title: created.title };
      state.model.tags.push(tag);
    } catch (error) {
      toast(`Could not create the tag “${name}”: ${error.message}`, "error");
      return;
    }
  }
  if (!(task.tagIDs || []).includes(tag.uuid)) await saveTags(uuid, [...(task.tagIDs || []), tag.uuid]);
}

// refreshEditorFooter redraws the footer of the open editor only.
function refreshEditorFooter(uuid) {
  const row = els.tasks.querySelector(`.task-row.editing[data-task="${CSS.escape(uuid)}"]`);
  const task = findTask(uuid);
  if (!row || !task) return;
  const holder = document.createElement("div");
  holder.innerHTML = editorFooter(task);
  row.querySelector(".editor-footer").replaceWith(holder.firstElementChild);
}

// reorderTask moves a to-do within its section at once and saves the new
// position; on failure the old order comes back.
async function reorderTask(moved, order) {
  const list = listForOrder();
  if (!list) return;
  const before = [...list];
  const at = order.indexOf(moved);
  const from = list.findIndex((task) => task.uuid === moved);
  if (from < 0) return;
  const [task] = list.splice(from, 1);
  const next = order[at + 1];
  const prev = order[at - 1];
  let to = next ? list.findIndex((item) => item.uuid === next) : -1;
  if (to < 0 && prev) to = list.findIndex((item) => item.uuid === prev) + 1;
  list.splice(to < 0 ? list.length : to, 0, task);
  render();
  try {
    await api("/api/reorder", {
      method: "POST",
      body: JSON.stringify({ view: state.scope ? "list" : state.view, moved, order }),
    });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    list.splice(0, list.length, ...before);
    render();
    toast(`Could not move “${task.title}”: ${error.message}`, "error");
  }
}

// reorderProject moves a project in the sidebar within its area at once and
// saves the new position.
async function reorderProject(moved, order) {
  const projects = state.model?.projects || [];
  const byUUID = new Map(projects.map((project) => [project.uuid, project]));
  const project = byUUID.get(moved);
  if (!project) return;
  const before = project.index;
  const at = order.indexOf(moved);
  const prev = byUUID.get(order[at - 1]);
  const next = byUUID.get(order[at + 1]);
  if (prev && next) project.index = (prev.index + next.index) / 2;
  else if (prev) project.index = prev.index + 1;
  else if (next) project.index = next.index - 1;
  renderSidebar();
  try {
    await api("/api/reorder", { method: "POST", body: JSON.stringify({ view: "projects", moved, order }) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    project.index = before;
    renderSidebar();
    toast(`Could not move “${project.title}”: ${error.message}`, "error");
  }
}

// moveTasks puts to-dos into a project, an area or a list at once and saves
// the move; on failure they go back.
async function moveTasks(uuids, target) {
  const tasks = uuids.map(findTask).filter(Boolean);
  if (!tasks.length) return;
  const before = tasks.map((task) => ({ ...task }));
  const lists = Object.fromEntries(Object.entries(state.model.lists).map(([view, rows]) => [view, [...rows]]));
  for (const task of tasks) {
    if (target.when) {
      task.views = { today: ["today", "anytime"], anytime: ["anytime"], someday: ["someday"] }[target.when];
    } else {
      if (target.heading) {
        const heading = state.model.headings.find((item) => item.uuid === target.heading);
        const project = state.model.projects.find((item) => item.uuid === heading?.project);
        Object.assign(task, { heading: target.heading, headingTitle: target.title, project: project?.uuid || "", projectTitle: project?.title || "" });
      } else if (target.project) {
        Object.assign(task, { project: target.project, projectTitle: target.title, heading: "", headingTitle: "" });
      } else {
        Object.assign(task, { project: "", projectTitle: "", area: target.area, areaTitle: target.title, heading: "", headingTitle: "" });
      }
      if (task.views.includes("inbox")) task.views = ["anytime"];
    }
  }
  for (const [view, rows] of Object.entries(state.model.lists)) {
    const moved = new Set(tasks.map((task) => task.uuid));
    state.model.lists[view] = rows.filter((row) => !moved.has(row.uuid) || row.views.includes(view));
    for (const task of tasks) {
      if (task.views.includes(view) && !state.model.lists[view].includes(task)) state.model.lists[view].push(task);
    }
  }
  state.selected.clear();
  render();
  try {
    const { title, quiet, ...where } = target;
    await api("/api/move", { method: "POST", body: JSON.stringify({ uuids: tasks.map((task) => task.uuid), ...where }) });
    if (!target.quiet) toast(tasks.length === 1 ? `Moved “${tasks[0].title}” to ${title}` : `Moved ${tasks.length} to-dos to ${title}`, "success");
    sync({ fresh: true, quiet: true });
    return true;
  } catch (error) {
    tasks.forEach((task, i) => {
      for (const key of Object.keys(task)) delete task[key];
      Object.assign(task, before[i]);
    });
    state.model.lists = lists;
    render();
    toast(`Could not move: ${error.message}`, "error");
    return false;
  }
}

// moveProjectArea puts a project into an area ("" for none) at once and
// saves it; with order it then takes its place among that area's projects.
async function moveProjectArea(uuid, area, order) {
  const project = state.model?.projects?.find((item) => item.uuid === uuid);
  if (!project || project.area === area) return;
  const before = { area: project.area, index: project.index };
  project.area = area;
  const siblings = state.model.projects.filter((item) => item !== project && item.area === area);
  project.index = siblings.length ? Math.max(...siblings.map((item) => item.index ?? 0)) + 1 : 0;
  renderSidebar();
  try {
    await api("/api/project-area", { method: "POST", body: JSON.stringify({ uuid, area }) });
    if (order) await reorderProject(uuid, order);
    else sync({ fresh: true, quiet: true });
  } catch (error) {
    Object.assign(project, before);
    renderSidebar();
    toast(`Could not move “${project.title}”: ${error.message}`, "error");
  }
}

// reorderArea moves an area in the sidebar at once and saves its position.
async function reorderArea(moved, order) {
  const areas = state.model?.areas || [];
  const byUUID = new Map(areas.map((area) => [area.uuid, area]));
  const area = byUUID.get(moved);
  if (!area) return;
  const before = area.index;
  const at = order.indexOf(moved);
  const prev = byUUID.get(order[at - 1]);
  const next = byUUID.get(order[at + 1]);
  if (prev && next) area.index = (prev.index + next.index) / 2;
  else if (prev) area.index = prev.index + 1;
  else if (next) area.index = next.index - 1;
  renderSidebar();
  try {
    await api("/api/reorder", { method: "POST", body: JSON.stringify({ view: "areas", moved, order }) });
    sync({ fresh: true, quiet: true });
  } catch (error) {
    area.index = before;
    renderSidebar();
    toast(`Could not move “${area.title}”: ${error.message}`, "error");
  }
}

// newArea asks for a name and creates an area.
function newArea() {
  if (els.projects.querySelector(".area-new")) return;
  const input = document.createElement("input");
  input.className = "area-new";
  input.placeholder = "Area name";
  input.maxLength = 200;
  input.setAttribute("aria-label", "Area name");
  els.projects.append(input);
  input.focus();
  let finished = false;
  const finish = async (save) => {
    if (finished) return;
    finished = true;
    const title = input.value.trim();
    input.remove();
    if (!save || !title) return;
    const areas = state.model.areas;
    const temp = { uuid: `pending-area-${++pendingId}`, title, type: "area", index: areas.length ? Math.max(...areas.map((area) => area.index ?? 0)) + 1 : 0 };
    areas.push(temp);
    renderSidebar();
    try {
      const result = await api("/api/area-create", { method: "POST", body: JSON.stringify({ title }) });
      temp.uuid = result.uuid;
      toast(`Created area “${title}”`, "success");
      sync({ fresh: true, quiet: true });
    } catch (error) {
      state.model.areas = areas.filter((area) => area !== temp);
      renderSidebar();
      toast(`Could not create “${title}”: ${error.message}`, "error");
    }
  };
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") finish(true);
    if (event.key === "Escape") finish(false);
  });
  input.addEventListener("blur", () => finish(true), { once: true });
}

// Dragging: one to-do is reordered within its section (mouse: the row;
// touch: the handle); the dragged to-do, or all selected ones, can be dropped
// onto a project, an area, Today, Anytime or Someday in the sidebar. Projects
// are reordered within their area in the sidebar (mouse).
const drag = { row: null, kind: "", uuids: [], startY: 0, active: false, target: null, into: null, after: false, suppressClick: false };

const dragKinds = {
  task: {
    selector: ".task-row[data-task]",
    key: (row) => row.dataset.task,
    peers: (row) => [...row.parentElement.querySelectorAll(".task-row[data-task]")],
    drop: (moved, order) => reorderTask(moved, order),
  },
  project: {
    selector: ".mini-item.project-row",
    key: (row) => row.dataset.scopeId,
    peers: (row) => [...els.projects.querySelectorAll(".mini-item.project-row")].filter((item) => item.dataset.group === row.dataset.group),
    drop: (moved, order) => reorderProject(moved, order),
  },
  item: {
    selector: ".check-item[data-item]:not(.new)",
    key: (row) => row.dataset.item,
    peers: (row) => [...row.parentElement.querySelectorAll(".check-item[data-item]:not(.new)")].filter((item) => item.dataset.item),
    drop: (moved, order, row) => checklistAction(row.closest(".task-row").dataset.task, "reorder", { uuid: moved, order }),
  },
  area: {
    selector: ".mini-item.area-row",
    key: (row) => row.dataset.scopeId,
    peers: () => [...els.projects.querySelectorAll(".mini-item.area-row")],
    drop: (moved, order) => reorderArea(moved, order),
  },
};

const movableViews = { today: "Today", anytime: "Anytime", someday: "Someday" };

// dropTarget returns where to-dos dropped on element would go.
// hoverExpand opens a folded area after a dragged to-do has rested on it
// for 600 ms, like Things.
let hoverArea = { uuid: "", timer: 0 };
function hoverExpand(element) {
  const area = element?.closest('.area-row[aria-expanded="false"]');
  const uuid = area?.dataset.scopeId || "";
  if (uuid === hoverArea.uuid) return;
  clearTimeout(hoverArea.timer);
  hoverArea = {
    uuid,
    timer: uuid
      ? setTimeout(() => {
          hoverArea = { uuid: "", timer: 0 };
          if (drag.active) toggleArea(uuid, true);
        }, 600)
      : 0,
  };
}

function dropTarget(element) {
  const heading = element?.closest("[data-heading-drop]");
  if (heading && isTemp(heading.dataset.headingDrop)) return null;
  if (heading) return { element: heading, target: { heading: heading.dataset.headingDrop, title: heading.dataset.headingTitle } };
  const scope = element?.closest(".mini-item[data-scope-type]");
  if (scope) {
    const key = scope.dataset.scopeType === "project" ? "project" : "area";
    return { element: scope, target: { [key]: scope.dataset.scopeId, title: scope.dataset.scopeTitle } };
  }
  const nav = element?.closest(".nav-item[data-view]");
  if (nav && movableViews[nav.dataset.view]) {
    return { element: nav, target: { when: nav.dataset.view, title: movableViews[nav.dataset.view] } };
  }
  return null;
}

let dragGhost = null;

function clearDropMarks() {
  document.querySelectorAll(".drop-before, .drop-after, .drop-into").forEach((row) => row.classList.remove("drop-before", "drop-after", "drop-into"));
}

// Touch: a long press selects a to-do (then taps add to the selection), or
// starts dragging a project or area in the sidebar. Moving the finger
// first scrolls as usual.
const longPress = { timer: 0, x: 0, y: 0 };
const longPressDelay = 450;

function cancelLongPress() {
  clearTimeout(longPress.timer);
  longPress.timer = 0;
}

function startLongPress(event) {
  const sidebarRow = event.target.closest(".mini-item.project-row, .mini-item.area-row");
  const taskRow = event.target.closest(".task-row[data-task]");
  if (!sidebarRow && (!taskRow || event.target.closest(".drag-handle, .check, .task-editor, a"))) return;
  Object.assign(longPress, { x: event.clientX, y: event.clientY });
  longPress.timer = setTimeout(() => {
    longPress.timer = 0;
    navigator.vibrate?.(15);
    state.skipClick = true;
    if (taskRow) {
      select(taskRow.dataset.task, { toggle: true });
      return;
    }
    const kind = sidebarRow.classList.contains("area-row") ? "area" : "project";
    Object.assign(drag, { row: sidebarRow, uuids: [], kind, startY: longPress.y, active: true, target: null, into: null });
    sidebarRow.classList.add("dragging");
    document.body.classList.add("is-dragging");
  }, longPressDelay);
}

function startDrag(event) {
  state.lastPointer = event.pointerType;
  if (event.pointerType !== "mouse" && !event.target.closest(".drag-handle, .item-handle")) {
    startLongPress(event);
  }
  const itemHandle = event.target.closest(".item-handle");
  if (itemHandle && event.button <= 0) {
    // Checklist items are dragged by their handle (mouse and touch).
    Object.assign(drag, { row: itemHandle.closest(".check-item"), uuids: [], kind: "item", startY: event.clientY, active: false, target: null, into: null });
    event.preventDefault();
    return;
  }
  if (!canReorder() || event.button > 0 || event.target.closest(".task-editor, .check, a")) return;
  const sidebarRow = event.target.closest(".mini-item.project-row, .mini-item.area-row");
  const projectRow = sidebarRow;
  const row = sidebarRow || event.target.closest(".task-row[data-task]");
  if (!row || (sidebarRow && event.pointerType !== "mouse")) return;
  const onHandle = Boolean(event.target.closest(".drag-handle"));
  if (!projectRow && event.pointerType !== "mouse" && !onHandle) return;
  let uuids = [];
  if (!projectRow) {
    const uuid = row.dataset.task;
    // Dragging a selected to-do takes the whole selection along.
    uuids = state.selected.has(uuid) && state.selected.size > 1 ? visibleRows().filter((id) => state.selected.has(id)) : [uuid];
  }
  const kind = sidebarRow ? (sidebarRow.classList.contains("area-row") ? "area" : "project") : "task";
  Object.assign(drag, { row, uuids, kind, startY: event.clientY, active: false, target: null, into: null });
  if (onHandle) event.preventDefault();
}

function moveDrag(event) {
  if (longPress.timer && Math.hypot(event.clientX - longPress.x, event.clientY - longPress.y) > 8) cancelLongPress();
  if (!drag.row) return;
  if (!drag.active) {
    if (Math.abs(event.clientY - drag.startY) < 5) return;
    drag.active = true;
    document.body.classList.add("is-dragging");
    if (drag.kind === "task") {
      for (const uuid of drag.uuids) {
        els.tasks.querySelector(`.task-row[data-task="${CSS.escape(uuid)}"]`)?.classList.add("dragging");
      }
      dragGhost = document.createElement("div");
      dragGhost.className = "drag-ghost";
      dragGhost.textContent = drag.uuids.length > 1 ? `${drag.uuids.length} to-dos` : findTask(drag.uuids[0])?.title || "";
      document.body.append(dragGhost);
    } else {
      drag.row.classList.add("dragging");
    }
  }
  event.preventDefault();
  if (dragGhost) {
    dragGhost.style.left = `${event.clientX + 14}px`;
    dragGhost.style.top = `${event.clientY + 10}px`;
  }
  if (event.clientY < 60) window.scrollBy(0, -12);
  if (event.clientY > window.innerHeight - 60) window.scrollBy(0, 12);
  clearDropMarks();
  drag.target = null;
  drag.into = null;
  const element = document.elementFromPoint(event.clientX, event.clientY);
  if (drag.kind === "task") {
    hoverExpand(element);
    const into = dropTarget(element);
    if (into) {
      drag.into = into.target;
      into.element.classList.add("drop-into");
      return;
    }
    if (drag.uuids.length > 1) return;
  }
  if (drag.kind === "project") {
    // A project dropped on an area moves into it.
    const area = element?.closest(".mini-item.area-row");
    if (area) {
      drag.into = { area: area.dataset.scopeId };
      area.classList.add("drop-into");
      return;
    }
    const other = element?.closest(".mini-item.project-row");
    if (other && other !== drag.row) {
      const box = other.getBoundingClientRect();
      drag.after = event.clientY > box.top + box.height / 2;
      drag.target = other;
      other.classList.add(drag.after ? "drop-after" : "drop-before");
    }
    return;
  }
  const kind = dragKinds[drag.kind];
  const under = element?.closest(kind.selector);
  // In a project, a to-do can also go into another heading's section.
  const otherSection =
    drag.kind === "task" &&
    under &&
    under.parentElement !== drag.row.parentElement &&
    !drag.uuids.includes(under.dataset.task) &&
    // In a project, into another heading's section; in Today, into or out
    // of This Evening.
    (state.scope?.type === "project" || (!state.scope && state.view === "today" && inEvening(under) !== inEvening(drag.row)));
  if (!under || under === drag.row || (!kind.peers(drag.row).includes(under) && !otherSection)) return;
  const box = under.getBoundingClientRect();
  drag.after = event.clientY > box.top + box.height / 2;
  drag.target = under;
  under.classList.add(drag.after ? "drop-after" : "drop-before");
}

function endDrag() {
  cancelLongPress();
  const { row, kind: kindName, uuids, active, target, into, after } = drag;
  drag.row = null;
  drag.active = false;
  clearDropMarks();
  dragGhost?.remove();
  dragGhost = null;
  document.body.classList.remove("is-dragging");
  document.querySelectorAll(".dragging").forEach((item) => item.classList.remove("dragging"));
  if (state.renderWaiting) setTimeout(renderTasks, 0);
  if (!row || !active) return;
  drag.suppressClick = true;
  setTimeout(() => (drag.suppressClick = false), 0);
  if (into && kindName === "project") {
    moveProjectArea(row.dataset.scopeId, into.area, null);
    return;
  }
  if (into) {
    moveTasks(uuids, into);
    return;
  }
  if (kindName === "project" && target && target.dataset.group !== row.dataset.group) {
    // Into another area (or out of any area), placed where it was dropped.
    const moved = row.dataset.scopeId;
    const order = dragKinds.project.peers(target).map((item) => item.dataset.scopeId);
    order.splice(order.indexOf(target.dataset.scopeId) + (after ? 1 : 0), 0, moved);
    moveProjectArea(moved, target.dataset.group, order);
    return;
  }
  if (!target) return;
  const kind = dragKinds[kindName];
  const moved = kind.key(row);
  if (kindName === "task" && !state.scope && state.view === "today" && inEvening(target) !== inEvening(row)) {
    // Into or out of This Evening, placed where it was dropped.
    const order = [...target.parentElement.querySelectorAll(".task-row[data-task]")].map((item) => item.dataset.task).filter((uuid) => !uuids.includes(uuid));
    order.splice(order.indexOf(target.dataset.task) + (after ? 1 : 0), 0, ...uuids);
    scheduleTasks(uuids, { when: inEvening(target) ? "evening" : "today" }).then((ok) => {
      if (ok && uuids.length === 1) reorderTask(moved, order);
    });
    return;
  }
  if (kindName === "task" && target.parentElement !== row.parentElement) {
    // Into another section of the project: move under its heading (or out of
    // the heading), then place it where it was dropped.
    const headingID = target.closest(".task-section").dataset.sectionHeading;
    const heading = state.model.headings.find((item) => item.uuid === headingID);
    const where = heading ? { heading: heading.uuid, title: heading.title } : { project: state.scope.id, title: state.scope.title };
    const order = [...target.parentElement.querySelectorAll(".task-row[data-task]")].map((item) => item.dataset.task).filter((uuid) => !uuids.includes(uuid));
    order.splice(order.indexOf(target.dataset.task) + (after ? 1 : 0), 0, ...uuids);
    moveTasks(uuids, { ...where, quiet: uuids.length === 1 }).then((ok) => {
      if (ok && uuids.length === 1) reorderTask(moved, order);
    });
    return;
  }
  const order = kind.peers(row).map(kind.key).filter((uuid) => uuid !== moved);
  order.splice(order.indexOf(kind.key(target)) + (after ? 1 : 0), 0, moved);
  kind.drop(moved, order, row);
}

// newHeading asks for a name and adds a heading at the end of the project.
function newHeading() {
  if (state.scope?.type !== "project" || els.tasks.querySelector(".heading-new")) return;
  const project = state.scope.id;
  const section = document.createElement("section");
  section.className = "task-section";
  section.innerHTML = `<input class="heading-new" maxlength="500" placeholder="Heading name" aria-label="Heading name" />`;
  els.tasks.append(section);
  const input = section.querySelector("input");
  input.focus();
  let finished = false;
  const finish = async (save) => {
    // Enter and the blur that follows must not both create it.
    if (finished) return;
    finished = true;
    const title = input.value.trim();
    section.remove();
    if (!save || !title) return;
    const temp = { uuid: `pending-heading-${++pendingId}`, title, project, index: Number.MAX_SAFE_INTEGER };
    state.model.headings = [...(state.model.headings || []), temp];
    renderTasks();
    try {
      const result = await api("/api/create", { method: "POST", body: JSON.stringify({ type: "heading", title, project }) });
      temp.uuid = result.uuid;
      renderTasks();
      sync({ fresh: true, quiet: true });
    } catch (error) {
      state.model.headings = state.model.headings.filter((item) => item !== temp);
      renderTasks();
      toast(`Could not add the heading “${title}”: ${error.message}`, "error");
    }
  };
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") finish(true);
    if (event.key === "Escape") finish(false);
  });
  input.addEventListener("blur", () => finish(true), { once: true });
}

function openNewProject() {
  const form = els.newProject.querySelector("form");
  form.reset();
  const areas = [...(state.model?.areas || [])].sort(byIndex);
  form.elements.area.innerHTML =
    `<option value="">No area</option>` +
    areas.filter((area) => !isTemp(area.uuid)).map((area) => `<option value="${escapeHtml(area.uuid)}">${escapeHtml(area.title)}</option>`).join("");
  if (state.scope?.type === "area") form.elements.area.value = state.scope.id;
  els.newProject.showModal();
  form.elements.title.focus();
}

// createProject creates the project and opens it once the sync lists it.
async function createProject(title, area) {
  try {
    const result = await api("/api/create", {
      method: "POST",
      body: JSON.stringify({ type: "project", title, area, when: "anytime" }),
    });
    toast(`Created project “${title}”`, "success");
    await sync({ fresh: true, quiet: true });
    const project = state.model?.projects?.find((item) => item.uuid === result.uuid);
    if (project) {
      state.scope = { type: "project", id: project.uuid, title: project.title };
      document.querySelectorAll(".nav-item").forEach((nav) => nav.classList.remove("active"));
      render();
    }
  } catch (error) {
    toast(`Could not create “${title}”: ${error.message}`, "error");
  }
}

function selectView(view) {
  if (typeof rememberPlace === "function" && (state.view !== view || state.scope)) rememberPlace();
  closeEditor();
  state.selected.clear();
  state.view = view;
  state.scope = null;
  if (archiveViews.includes(view)) {
    state.archive[view] = null;
    loadArchive(view);
  }
  document.querySelectorAll(".nav-item").forEach((item) => {
    item.classList.toggle("active", item.dataset.view === view);
  });
  try {
    localStorage.setItem("things-view", view);
  } catch {
    // Storage is optional.
  }
  render();
}

function typing(target) {
  return target instanceof HTMLElement && (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName));
}

async function init() {
  fillIcons();
  try {
    const saved = localStorage.getItem("things-view");
    if (viewKeys.includes(saved)) selectView(saved);
  } catch {
    // Storage is optional.
  }

  document.querySelectorAll(".nav-item").forEach((button) => {
    button.addEventListener("click", () => selectView(button.dataset.view));
  });

  document.addEventListener("pointerdown", startDrag);
  document.addEventListener("pointermove", moveDrag, { passive: false });
  document.addEventListener("pointerup", endDrag);
  document.addEventListener("pointercancel", endDrag);
  // A drag released outside the window ends too.
  window.addEventListener("blur", endDrag);
  // Once a touch drag runs, the finger must not scroll the page.
  document.addEventListener(
    "touchmove",
    (event) => {
      if (drag.active || (drag.row && drag.kind === "item")) event.preventDefault();
    },
    { passive: false },
  );
  // No native drag of rows, links or text in the lists: it would cancel
  // moving to-dos (the editor keeps it).
  document.addEventListener("dragstart", (event) => {
    const element = event.target instanceof Element ? event.target : event.target.parentElement;
    if (element?.closest(".task-row, .mini-item, .nav-item") && !element.closest(".task-editor")) event.preventDefault();
  });

  document.addEventListener("click", (event) => {
    if (drag.suppressClick || state.skipClick) {
      state.skipClick = false;
      event.preventDefault();
      return;
    }
    if (event.target.closest("[data-empty-trash]")) {
      emptyTrash();
      return;
    }
    if (event.target.closest("[data-archive-more]")) {
      loadArchive(state.view, Math.min((state.archive[state.view]?.limit || 300) + 300, 2000));
      return;
    }
    const tagFilter = event.target.closest("[data-tag-filter]");
    if (tagFilter) {
      toggleTagFilter(tagFilter.dataset.tagFilter);
      return;
    }
    const archived = event.target.closest("[data-reopen], [data-restore]");
    if (archived) {
      archiveAction(archived.dataset.reopen || archived.dataset.restore, Boolean(archived.dataset.restore));
      return;
    }
    const completeButton = event.target.closest("[data-complete]");
    if (completeButton) {
      // Alt+click cancels, as in Things.
      if (event.altKey) cancelTasks([completeButton.dataset.complete]);
      else toggleComplete(completeButton.dataset.complete);
      return;
    }
    const cancelButton = event.target.closest("[data-cancel-task]");
    if (cancelButton) {
      const uuid = cancelButton.dataset.cancelTask;
      Promise.resolve(cancelTasks([uuid])).then(() => refreshEditorFooter(uuid));
      return;
    }
    const itemToggle = event.target.closest("[data-item-toggle]");
    if (itemToggle) {
      const taskUUID = itemToggle.closest(".task-row").dataset.task;
      const item = findTask(taskUUID)?.checklist?.find((entry) => entry.uuid === itemToggle.dataset.itemToggle);
      if (item) checklistAction(taskUUID, "complete", { uuid: item.uuid, done: !item.done });
      return;
    }
    if (event.target.closest(".task-editor, a")) return;

    const row = event.target.closest(".task-row[data-task]");
    if (row) {
      const uuid = row.dataset.task;
      if (state.editing && state.editing !== uuid) closeEditor();
      // Touch has no double click: a tap opens the to-do, or, while to-dos
      // are selected (after a long press), adds it to the selection.
      const touch = state.lastPointer === "touch" || state.lastPointer === "pen";
      if (touch && state.selected.size) select(uuid, { toggle: true });
      else if (touch) openEditor(uuid);
      else if (state.editing !== uuid) select(uuid, { extend: event.shiftKey, toggle: event.metaKey || event.ctrlKey });
      return;
    }

    // The triangle folds an area; a click on its name still opens it.
    const fold = event.target.closest("[data-toggle-area]");
    if (fold) {
      event.preventDefault();
      toggleArea(fold.dataset.toggleArea);
      return;
    }
    const item = event.target.closest("[data-scope-type]");
    if (!item) {
      // A click outside the to-dos closes the editor and clears the selection.
      if (event.target.closest(".content") && !event.target.closest("form, button, input, textarea, .toast")) {
        closeEditor();
        if (state.selected.size) {
          state.selected.clear();
          renderSelection();
        }
      }
      return;
    }
    // A double click on a sidebar project or area renames it. The first
    // click re-rendered the sidebar, so no dblclick event arrives.
    if (event.detail === 2 && item.closest("#projects")) {
      renameInSidebar(item.dataset.scopeType, item.dataset.scopeId);
      return;
    }
    closeEditor();
    state.selected.clear();
    state.sidebarProject = item.closest("#projects") && item.dataset.scopeType === "project" ? item.dataset.scopeId : "";
    rememberPlace();
    state.scope = {
      type: item.dataset.scopeType,
      id: item.dataset.scopeId,
      title: item.dataset.scopeTitle,
    };
    document.querySelectorAll(".nav-item").forEach((nav) => nav.classList.remove("active"));
    render();
  });

  document.addEventListener("dblclick", (event) => {
    // A to-do still being created opens as soon as it exists.
    const pending = event.target.closest(".task-row[data-pending]");
    if (pending) {
      state.openWhenReady = pending.dataset.pending;
      return;
    }
    const heading = event.target.closest("h2.heading[data-heading-drop]");
    if (heading && !event.target.closest("input")) {
      renameHeading(heading.dataset.headingDrop);
      return;
    }
    const row = event.target.closest(".task-row[data-task]");
    if (row && !event.target.closest(".task-editor, .check, a")) openEditor(row.dataset.task);
  });

  // Editor inputs: save the title on Enter or when leaving it, notes while
  // typing, checklist items on Enter or when leaving them.
  els.tasks.addEventListener("input", (event) => {
    const note = event.target.closest(".edit-note");
    if (!note) return;
    autosize(note);
    // The Markdown below follows what is typed.
    const preview = note.parentElement.querySelector(".note-markdown, .note-links");
    const next = note.value.trim() ? notePreview(note.value) : "";
    if (preview) preview.outerHTML = next || `<div class="note-links"></div>`;
    else if (next) note.insertAdjacentHTML("afterend", next);
    const uuid = note.closest(".task-row").dataset.task;
    clearTimeout(noteTimer);
    noteTimer = setTimeout(() => {
      const value = note.value.replace(/\s+$/, "");
      if (value !== (findTask(uuid)?.note || "")) saveTask(uuid, { note: value });
    }, 800);
  });
  els.tasks.addEventListener("change", (event) => {
    const row = event.target.closest(".task-row.editing");
    if (!row) return;
    const uuid = row.dataset.task;
    if (event.target.classList.contains("edit-when")) {
      const start = row.querySelector(".edit-start");
      if (event.target.value === "date") {
        start.hidden = false;
        start.focus();
        start.showPicker?.();
      } else {
        start.hidden = true;
        scheduleTasks([uuid], { when: event.target.value }).then(() => refreshEditorFooter(uuid));
      }
    } else if (event.target.classList.contains("edit-start") && event.target.value) {
      scheduleTasks([uuid], { startDate: event.target.value }).then(() => refreshEditorFooter(uuid));
    } else if (event.target.classList.contains("edit-deadline")) {
      saveDeadline(uuid, event.target.value);
    }
  });
  els.tasks.addEventListener("click", (event) => {
    const row = event.target.closest(".task-row.editing");
    if (!row) return;
    const remove = event.target.closest("[data-tag-remove]");
    if (remove) {
      const task = findTask(row.dataset.task);
      saveTags(row.dataset.task, (task.tagIDs || []).filter((id) => id !== remove.dataset.tagRemove));
    } else if (event.target.closest("[data-clear-deadline]")) {
      saveDeadline(row.dataset.task, "");
    } else if (event.target.closest("[data-clear-reminder]")) {
      saveReminder(row.dataset.task, "");
    } else if (event.target.closest("[data-repeat-task]")) {
      openRepeat(row.dataset.task);
    } else if (event.target.closest("[data-convert-task]")) {
      convertToProject(row.dataset.task);
    }
  });

  els.tasks.addEventListener("focusout", (event) => {
    const input = event.target;
    const row = input.closest?.(".task-row.editing");
    if (!row) return;
    const uuid = row.dataset.task;
    const task = findTask(uuid);
    if (!task) return;
    // A time field changes with every typed digit; save when it is left.
    if (input.classList.contains("edit-reminder")) {
      saveReminder(uuid, input.value);
    } else if (input.classList.contains("edit-title")) {
      const title = input.value.trim();
      if (title && title !== task.title) saveTask(uuid, { title });
    } else if (input.classList.contains("item-title")) {
      const itemUUID = input.closest(".check-item").dataset.item;
      const item = task.checklist?.find((entry) => entry.uuid === itemUUID);
      const title = input.value.trim();
      if (item && title && title !== item.title) checklistAction(uuid, "edit", { uuid: itemUUID, title });
    } else if (input.classList.contains("item-new") && input.value.trim()) {
      checklistAction(uuid, "add", { title: input.value.trim() });
    }
    // Render what waited once focus has left the editor.
    setTimeout(() => {
      if (state.renderWaiting && !editorInUse()) renderTasks();
    }, 0);
  });
  els.tasks.addEventListener("keydown", (event) => {
    const input = event.target;
    const row = input.closest?.(".task-row.editing");
    if (!row) return;
    const uuid = row.dataset.task;
    if (event.key === "Escape" || ((event.metaKey || event.ctrlKey) && event.key === "Enter")) {
      event.preventDefault();
      event.stopPropagation();
      closeEditor();
      return;
    }
    if (input.classList.contains("tag-new") && event.key === "Enter") {
      event.preventDefault();
      const name = input.value.trim();
      input.value = "";
      addTag(uuid, name);
      return;
    }
    if (event.key !== "Enter" && event.key !== "Backspace") return;
    if (input.classList.contains("edit-title") && event.key === "Enter") {
      event.preventDefault();
      row.querySelector(".edit-note").focus();
    } else if (input.classList.contains("item-new") && event.key === "Enter") {
      event.preventDefault();
      const title = input.value.trim();
      if (title) {
        input.value = "";
        checklistAction(uuid, "add", { title });
      }
    } else if (input.classList.contains("item-title")) {
      const itemUUID = input.closest(".check-item").dataset.item;
      if (event.key === "Enter") {
        event.preventDefault();
        row.querySelector(".item-new").focus();
      } else if (event.key === "Backspace" && !input.value && itemUUID) {
        event.preventDefault();
        checklistAction(uuid, "delete", { uuid: itemUUID });
      }
    }
  });

  // Shortcuts as in Things (shortcuts.js; Ctrl stands for Cmd on Windows and
  // Linux).
  document.addEventListener("keydown", handleShortcut);
  els.shortcutsButton.addEventListener("click", openCheatSheet);

  els.search.addEventListener("input", () => {
    state.query = els.search.value;
    searchLogbook();
    renderTasks();
  });

  els.refresh.addEventListener("click", () => {
    sync({ fresh: true }).then(() => {
      if (inArchive()) loadArchive(state.view);
    });
  });
  els.newProjectButton.addEventListener("click", openNewProject);
  els.newAreaButton.addEventListener("click", newArea);
  els.tagsButton.addEventListener("click", openTags);
  els.tagsDialog.addEventListener("click", (event) => {
    const remove = event.target.closest("[data-tag-delete]");
    if (remove) deleteTag(remove.dataset.tagDelete);
  });
  els.tagsDialog.addEventListener("change", (event) => {
    const input = event.target.closest("[data-tag-rename]");
    if (input) renameTag(input.dataset.tagRename, input.value);
  });
  els.tagsDialog.addEventListener("keydown", (event) => {
    // Enter saves the name instead of closing the dialog.
    if (event.key === "Enter" && event.target.closest("[data-tag-rename]")) {
      event.preventDefault();
      event.target.blur();
    }
  });
  els.logCompleted.addEventListener("click", logCompleted);
  els.newHeading.addEventListener("click", newHeading);
  els.completeProject.addEventListener("click", () => {
    if (state.scope?.type === "project") finishProject(state.scope.id, "complete");
  });
  els.projectDetails.addEventListener("change", (event) => {
    const uuid = els.projectDetails.dataset.project;
    if (event.target.classList.contains("project-when")) {
      const start = els.projectDetails.querySelector(".project-start");
      if (event.target.value === "date") {
        start.hidden = false;
        start.focus();
        start.showPicker?.();
      } else {
        event.target.blur();
        scheduleProject(uuid, { when: event.target.value });
      }
    } else if (event.target.classList.contains("project-start") && event.target.value) {
      event.target.blur();
      scheduleProject(uuid, { startDate: event.target.value });
    } else if (event.target.classList.contains("project-deadline")) {
      const deadline = event.target.value;
      event.target.blur();
      saveProjectDetail(uuid, "/api/deadline", { uuid, deadline }, (project) => (project.deadline = deadline));
    }
  });
  els.projectDetails.addEventListener("click", (event) => {
    const remove = event.target.closest("[data-project-tag-remove]");
    if (!remove) return;
    const uuid = els.projectDetails.dataset.project;
    const project = headerItem(uuid);
    // The header is not redrawn while it has the focus.
    remove.blur();
    if (project) setProjectTags(uuid, (project.tagIDs || []).filter((id) => id !== remove.dataset.projectTagRemove));
  });
  els.projectDetails.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" || !event.target.classList.contains("project-tag-new")) return;
    event.preventDefault();
    const name = event.target.value.trim();
    event.target.value = "";
    event.target.blur();
    addProjectTag(els.projectDetails.dataset.project, name);
  });
  els.deleteProject.addEventListener("click", () => {
    if (state.scope?.type === "project") deleteProject(state.scope.id);
  });
  els.projects.addEventListener("contextmenu", projectMenu);
  els.projects.addEventListener("keydown", (event) => {
    // Left folds a focused area, Right opens it.
    const area = event.target.closest?.(".area-row[data-scope-id]");
    if (!area || (event.key !== "ArrowLeft" && event.key !== "ArrowRight") || event.metaKey || event.ctrlKey || event.altKey) return;
    event.preventDefault();
    event.stopPropagation();
    toggleArea(area.dataset.scopeId, event.key === "ArrowRight");
  });
  els.tasks.addEventListener("contextmenu", headingMenu);
  els.viewTitle.addEventListener("click", renameInHeader);
  els.projectNote.addEventListener("input", () => growNote(els.projectNote));
  els.projectNote.addEventListener("blur", saveProjectNote);
  els.projectNote.addEventListener("keydown", (event) => {
    if (event.key === "Escape") els.projectNote.blur();
  });
  document.addEventListener("pointerdown", (event) => {
    if (!event.target.closest?.(".context-menu")) closeProjectMenu();
    // Clicking elsewhere ends the sidebar selection of a project.
    if (!event.target.closest?.("#projects")) state.sidebarProject = "";
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") closeProjectMenu();
  });
  document.querySelector("#new-project-cancel").addEventListener("click", () => els.newProject.close("cancel"));
  document.querySelector("#repeat-cancel").addEventListener("click", () => els.repeatDialog.close(""));
  els.repeatDialog.querySelector("select[name=unit]").addEventListener("change", (event) => {
    document.querySelector("#repeat-month").hidden = event.target.value !== "month";
  });
  els.newProject.addEventListener("close", () => {
    if (els.newProject.returnValue !== "create") return;
    const form = els.newProject.querySelector("form");
    const title = form.elements.title.value.trim();
    if (title) createProject(title, form.elements.area.value);
  });
  els.settings.addEventListener("click", async () => {
    try {
      await api("/api/logout", { method: "POST" });
      state.model = null;
      state.phase = "signedOut";
      state.scope = null;
      state.query = "";
      state.syncedAt = null;
      els.search.value = "";
      els.summary.textContent = "No sync yet";
      setStatus("Disconnected", false);
      render();
    } catch (error) {
      toast(error.message, "error");
    }
  });
  els.quickAdd.addEventListener("submit", (event) => {
    event.preventDefault();
    createTask(els.quickAdd);
  });

  els.loginForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(els.loginForm);
    setStatus("Connecting…", false, true);
    try {
      const login = await api("/api/login", {
        method: "POST",
        body: JSON.stringify({
          email: form.get("email"),
          password: form.get("password"),
        }),
      });
      setBackendNote(login.backend);
      state.phase = "loading";
      render();
      await sync();
    } catch (error) {
      setStatus("Disconnected", false);
      els.summary.textContent = error.message;
    }
  });

  document.addEventListener("visibilitychange", refreshIfStale);
  window.addEventListener("focus", refreshIfStale);
  setInterval(() => {
    if (document.visibilityState === "visible" && state.phase === "ready") sync({ fresh: true, quiet: true });
  }, refreshEvery);

  render();
  const status = await api("/api/status");
  setBackendNote(status.backend);
  // The server owns the Things account, so there is nothing to sign out of.
  els.settings.hidden = Boolean(status.serverManaged);
  els.account.hidden = status.auth !== "passkey";
  if (!status.signedIn) {
    state.phase = "signedOut";
    setStatus("Disconnected", false);
    render();
    return;
  }
  await sync();
}

init().catch((error) => {
  state.phase = state.model ? "ready" : "signedOut";
  setStatus("Disconnected", false);
  els.summary.textContent = error.message;
  render();
});
