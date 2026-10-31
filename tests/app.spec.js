const { test, expect } = require("@playwright/test");

const emptyModel = {
  generatedAt: "2026-08-17T12:00:00Z",
  tasks: [],
  projects: [],
  areas: [],
  lists: {
    today: [],
    inbox: [],
    anytime: [],
    upcoming: [],
    someday: [],
  },
};

test("quick add is hidden when the current view cannot accept tasks", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Connect Things Cloud" })).toBeVisible();
  await expect(page.locator("#quick-add")).toBeHidden();
});

test("settings disconnects the active Things Cloud session", async ({ page }) => {
  let loggedOut = false;
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/status") {
      await route.fulfill({ json: { signedIn: true, email: "user@example.com", backend: {} } });
      return;
    }
    if (path === "/api/sync") {
      await route.fulfill({ json: emptyModel });
      return;
    }
    if (path === "/api/logout" && request.method() === "POST") {
      loggedOut = true;
      await route.fulfill({ json: { signedIn: false } });
      return;
    }
    await route.abort();
  });

  await page.goto("/");
  await expect(page.locator("#status-text")).toHaveText(/^Synced /);

  await page.getByRole("button", { name: "Settings" }).click();

  await expect.poll(() => loggedOut).toBe(true);
  await expect(page.locator("#status-text")).toHaveText("Disconnected");
  await expect(page.locator("#summary")).toHaveText("No sync yet");
  await expect(page.getByRole("heading", { name: "Connect Things Cloud" })).toBeVisible();
});

function task(uuid, title, views = ["today", "anytime"]) {
  return {
    uuid,
    title,
    note: "",
    status: "open",
    type: "task",
    schedule: views[0],
    scheduledDate: "",
    deadline: "",
    project: "",
    projectTitle: "",
    heading: "",
    headingTitle: "",
    area: "",
    areaTitle: "",
    tags: [],
    views,
  };
}

function model(tasks, extra = {}) {
  const lists = { today: [], inbox: [], anytime: [], upcoming: [], someday: [] };
  for (const item of tasks) for (const view of item.views) lists[view].push(item);
  return { ...emptyModel, generatedAt: new Date().toISOString(), syncedAt: new Date().toISOString(), tasks, lists, ...extra };
}

// mockApp serves /api/* from handlers; unhandled paths abort.
async function mockApp(page, handlers) {
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const key = `${request.method()} ${url.pathname}${url.searchParams.get("fresh") ? "?fresh" : ""}`;
    const handler = handlers[key] || handlers[`${request.method()} ${url.pathname}`];
    if (!handler) return route.abort();
    return handler(route, request);
  });
}

const signedIn = (route) => route.fulfill({ json: { signedIn: true, serverManaged: true, backend: { cli: "things-cli" } } });

function deferred() {
  let resolve;
  const promise = new Promise((done) => (resolve = done));
  return { promise, resolve };
}

test("shows a loading skeleton instead of the sign-in form while the first sync runs", async ({ page }) => {
  const release = deferred();
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": async (route) => {
      await release.promise;
      await route.fulfill({ json: model([task("t1", "Water plants")]) });
    },
  });

  await page.goto("/");
  await expect(page.locator("#loading")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Connect Things Cloud" })).toBeHidden();
  await expect(page.locator("#status-text")).toHaveText("Loading…");

  release.resolve();
  await expect(page.locator(".task-title", { hasText: "Water plants" })).toBeVisible();
  await expect(page.locator("#loading")).toBeHidden();
});

test("renders the cached model at once and replaces it after the refresh", async ({ page }) => {
  const release = deferred();
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("t1", "Cached task")], { refreshing: true }) }),
    "GET /api/sync?fresh": async (route) => {
      await release.promise;
      await route.fulfill({ json: model([task("t1", "Cached task"), task("t2", "New from cloud")]) });
    },
  });

  await page.goto("/");
  await expect(page.locator(".task-title", { hasText: "Cached task" })).toBeVisible();
  await expect(page.locator("#status-text")).toHaveText("Updating…");
  release.resolve();
  await expect(page.locator(".task-title", { hasText: "New from cloud" })).toBeVisible();
  await expect(page.locator("#status-text")).toHaveText(/^Synced /);
});

test("completing a to-do ticks it off and keeps it in the list until logged", async ({ page }) => {
  const release = deferred();
  let completed = "";
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) =>
      route.fulfill({
        json: model([{ ...task("t1", "Finish report"), status: completed ? "completed" : "open" }, task("t2", "Stays")]),
      }),
    "POST /api/complete": async (route, request) => {
      await release.promise;
      completed = request.postDataJSON().uuid;
      await route.fulfill({ json: { completed: true, uuid: completed } });
    },
  });

  await page.goto("/");
  await page.getByRole("button", { name: "Complete Finish report" }).click();
  const row = page.locator(".task-row", { hasText: "Finish report" });
  await expect(row).toHaveClass(/done/);
  await expect(page.locator("#count-today")).toHaveText("1");
  release.resolve();
  await expect.poll(() => completed).toBe("t1");
  await expect(row).toHaveClass(/done/);
  await expect(page.locator("#log-completed")).toBeVisible();
});

test("cancelling: Ctrl+Alt+K, Alt+click and the editor button cross a to-do out and reopen it", async ({ page }) => {
  const calls = [];
  const tasks = [task("a", "Alpha"), task("b", "Beta"), task("c", "Gamma")];
  const record = (route, request) => {
    const body = request.postDataJSON();
    calls.push({ path: new URL(request.url()).pathname, body });
    for (const item of tasks) {
      if (body.uuids?.includes(item.uuid)) item.status = "canceled";
      if (body.uuid === item.uuid) item.status = "open";
    }
    return route.fulfill({ json: { ok: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model(structuredClone(tasks)) }),
    "POST /api/cancel": record,
    "POST /api/reopen": record,
  });

  await page.goto("/");
  await page.locator(".task-row", { hasText: "Alpha" }).click();
  await page.locator(".task-row", { hasText: "Beta" }).click({ modifiers: ["Shift"] });
  await page.keyboard.press("Control+Alt+k");
  await expect.poll(() => calls[0]).toEqual({ path: "/api/cancel", body: { uuids: ["a", "b"] } });
  await expect(page.locator(".task-row.canceled .task-title")).toHaveText(["Alpha", "Beta"]);
  // Canceled to-dos stay until they are logged.
  await expect(page.locator("#log-completed")).toBeVisible();

  await page.locator(".task-row", { hasText: "Gamma" }).locator(".check").click({ modifiers: ["Alt"] });
  await expect.poll(() => calls[1]).toEqual({ path: "/api/cancel", body: { uuids: ["c"] } });
  await expect(page.locator(".task-row", { hasText: "Gamma" })).toHaveClass(/canceled/);

  await page.locator(".task-row", { hasText: "Gamma" }).dblclick();
  await page.locator(".task-row.editing [data-cancel-task]").click();
  await expect.poll(() => calls[2]).toEqual({ path: "/api/reopen", body: { uuid: "c" } });
  await expect(page.locator(".task-row.editing [data-cancel-task]")).toHaveText("Cancel To-Do");
  await page.keyboard.press("Escape");
  await expect(page.locator(".task-row.editing")).toHaveCount(0);
  await expect(page.locator(".task-row", { hasText: "Gamma" })).not.toHaveClass(/canceled/);
});

test("a failed completion unticks the to-do again", async ({ page }) => {
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("t1", "Finish report")]) }),
    "POST /api/complete": (route) => route.fulfill({ status: 502, json: { error: "Things Cloud is unreachable." } }),
  });

  await page.goto("/");
  await page.getByRole("button", { name: "Complete Finish report" }).click();
  await expect(page.locator(".toast.error")).toContainText("Things Cloud is unreachable.");
  await expect(page.locator(".task-row", { hasText: "Finish report" })).not.toHaveClass(/done/);
  await expect(page.locator("#count-today")).toHaveText("1");
});

test("Log Completed moves completed to-dos out of the list", async ({ page }) => {
  let logged = false;
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) =>
      route.fulfill({ json: model(logged ? [task("t2", "Open")] : [{ ...task("t1", "Done earlier"), status: "completed" }, task("t2", "Open")]) }),
    "POST /api/log-completed": (route) => {
      logged = true;
      return route.fulfill({ json: { ok: true } });
    },
  });
  await page.goto("/");
  await expect(page.locator(".task-row.done")).toHaveCount(1);
  await page.getByRole("button", { name: "Log Completed" }).click();
  await expect(page.locator(".task-row.done")).toHaveCount(0);
  await expect(page.locator("#log-completed")).toBeHidden();
  expect(logged).toBe(true);
});

test("quick add shows the new task at once and keeps the form ready", async ({ page }) => {
  const release = deferred();
  let created = false;
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) =>
      route.fulfill({ json: model(created ? [task("t9", "Buy milk", ["inbox"])] : []) }),
    "POST /api/create": async (route) => {
      await release.promise;
      created = true;
      await route.fulfill({ json: { created: true, when: "inbox" } });
    },
  });

  await page.goto("/");
  await page.keyboard.press("1");
  await expect(page.locator("#view-title")).toHaveText("Inbox");
  await page.keyboard.press("n");
  await page.keyboard.type("Buy milk");
  await page.keyboard.press("Enter");

  await expect(page.locator(".task-row.pending")).toContainText("Buy milk");
  await expect(page.locator("#quick-add input[name=title]")).toHaveValue("");
  await expect(page.locator("#quick-add input[name=title]")).toBeFocused();
  release.resolve();
  await expect(page.locator(".task-row.pending")).toHaveCount(0);
  await expect(page.locator(".task-row[data-task=t9]")).toContainText("Buy milk");
});

test("keyboard shortcuts switch lists and focus search", async ({ page }) => {
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("t1", "Alpha"), task("t2", "Beta")]) }),
  });

  await page.goto("/");
  await expect(page.locator(".task-title", { hasText: "Alpha" })).toBeVisible();
  await page.keyboard.press("3");
  await expect(page.locator("#view-title")).toHaveText("Upcoming");
  await page.keyboard.press("2");
  await page.keyboard.press("/");
  await expect(page.locator("#search")).toBeFocused();
  await page.keyboard.type("bet");
  await expect(page.locator(".task-title", { hasText: "Alpha" })).toBeHidden();
  await expect(page.locator(".task-title", { hasText: "Beta" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".task-title", { hasText: "Alpha" })).toBeVisible();
});

test("the sidebar lists active projects by area like Things and Someday shows Someday projects", async ({ page }) => {
  const inProject = { ...task("t1", "Cable ducts", ["anytime"]), project: "p-august", projectTitle: "Summer", area: "a-private" };
  const data = model([inProject, task("t2", "Loose")], {
    areas: [
      { uuid: "a-pub", title: "Papers", index: 5, count: 0 },
      { uuid: "a-private", title: "Private", index: 1, count: 1 },
    ],
    projects: [
      { uuid: "p-trip", title: "City Trip", index: -90, area: "a-private", when: "anytime", count: 0 },
      { uuid: "p-august", title: "Summer", index: -80, area: "a-private", when: "anytime", count: 1 },
      { uuid: "p-work", title: "Work", index: 3, area: "", when: "anytime", count: 0 },
      { uuid: "p-sensor", title: "SensorRig", index: -95, area: "a-pub", when: "someday", count: 0 },
    ],
  });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
  });

  await page.goto("/");
  await page.keyboard.press("4");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Work", "Private", "City Trip", "Summer", "Papers"]);
  await expect(page.locator("#projects")).not.toContainText("SensorRig");

  await page.keyboard.press("5");
  await page.locator(".project-link", { hasText: "SensorRig" }).click();
  await expect(page.locator("#view-title")).toHaveText("SensorRig");

  // The sidebar is the same in every list, as in Things.
  await page.keyboard.press("2");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Work", "Private", "City Trip", "Summer", "Papers"]);
});

test("new to-dos can be added in every list; Upcoming asks for a start date", async ({ page }) => {
  const bodies = [];
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([]) }),
    "POST /api/create": (route, request) => {
      bodies.push(request.postDataJSON());
      return route.fulfill({ json: { created: true } });
    },
  });

  await page.goto("/");
  await expect(page.locator("#quick-add")).toBeVisible();
  await page.keyboard.press("n");
  await page.keyboard.type("Call mum");
  await page.keyboard.press("Enter");

  await page.keyboard.press("Escape");
  await page.locator("#quick-add input[name=title]").blur();
  await page.keyboard.press("3");
  const date = page.locator("#quick-add input[name=scheduled]");
  await expect(date).toBeVisible();
  await date.fill("2030-01-15");
  await page.locator("#quick-add input[name=title]").fill("Renew passport");
  await page.locator("#quick-add input[name=title]").press("Enter");

  await expect.poll(() => bodies.length).toBe(2);
  expect(bodies[0]).toMatchObject({ title: "Call mum", when: "today" });
  expect(bodies[1]).toMatchObject({ title: "Renew passport", scheduled: "2030-01-15" });
  expect(bodies[1].when).toBeUndefined();
});

test("a new project is created in the chosen area and opened", async ({ page }) => {
  let created = null;
  const areas = [{ uuid: "a-private", title: "Private", index: 1, count: 0 }];
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) =>
      route.fulfill({
        json: model([], {
          areas,
          projects: created
            ? [{ uuid: "p-new", title: created.title, index: 0, area: created.area, when: "anytime", count: 0 }]
            : [],
        }),
      }),
    "POST /api/create": (route, request) => {
      created = request.postDataJSON();
      return route.fulfill({ json: { created: true, uuid: "p-new" } });
    },
  });

  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();
  await page.locator("#new-project input[name=title]").fill("Garden");
  await page.locator("#new-project select[name=area]").selectOption("a-private");
  await page.locator("#new-project").getByRole("button", { name: "Create" }).click();

  await expect(page.locator("#view-title")).toHaveText("Garden");
  expect(created).toMatchObject({ type: "project", title: "Garden", area: "a-private" });
  await expect(page.locator("#projects .mini-title")).toHaveText(["Private", "Garden"]);
});

async function dragRow(page, from, to, { after = false } = {}) {
  const a = await page.locator(".task-row", { hasText: from }).boundingBox();
  const b = await page.locator(".task-row", { hasText: to }).boundingBox();
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.mouse.down();
  const y = after ? b.y + b.height * 0.8 : b.y + b.height * 0.2;
  await page.mouse.move(b.x + b.width / 2, y, { steps: 8 });
  await page.mouse.up();
}

test("dragging a to-do reorders it at once and saves the section order", async ({ page }) => {
  let request = null;
  // Like the server, the sync after the write shows the new order.
  const tasks = [task("a", "Alpha"), task("b", "Beta"), task("c", "Gamma")];
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model(tasks) }),
    "POST /api/reorder": (route, req) => {
      request = req.postDataJSON();
      tasks.sort((a, b) => request.order.indexOf(a.uuid) - request.order.indexOf(b.uuid));
      return route.fulfill({ json: { reordered: true } });
    },
  });

  await page.goto("/");
  await expect(page.locator(".task-title")).toHaveText(["Alpha", "Beta", "Gamma"]);
  await dragRow(page, "Gamma", "Alpha");
  await expect(page.locator(".task-title")).toHaveText(["Gamma", "Alpha", "Beta"]);
  await expect.poll(() => request).toEqual({ view: "today", moved: "c", order: ["c", "a", "b"] });
  // The drag did not open the to-do.
  await expect(page.locator(".task-row.editing")).toHaveCount(0);
});

test("This Evening: its own section in Today, Ctrl+E, the When menu and dragging in and out", async ({ page }) => {
  const calls = [];
  // The server keeps what was written, so later syncs show it.
  const tasks = [task("a", "Alpha"), task("b", "Beta"), { ...task("c", "Gamma"), evening: true }];
  let syncs = 0;
  const record = (route, request) => {
    const body = request.postDataJSON();
    calls.push({ path: new URL(request.url()).pathname, body });
    for (const item of tasks) if (body.uuids?.includes(item.uuid)) item.evening = body.when === "evening";
    if (body.moved) tasks.unshift(...tasks.splice(tasks.findIndex((item) => item.uuid === body.moved), 1));
    return route.fulfill({ json: { ok: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => {
      syncs += 1;
      return route.fulfill({ json: model(structuredClone(tasks)) });
    },
    "POST /api/schedule": record,
    "POST /api/reorder": record,
  });

  await page.goto("/");
  const evening = page.locator(".task-section.evening");
  await expect(evening.locator("h2")).toHaveText("This Evening");
  await expect(evening.locator(".task-title")).toHaveText(["Gamma"]);
  await expect(page.locator(".task-title")).toHaveText(["Alpha", "Beta", "Gamma"]);

  await page.locator(".task-row", { hasText: "Alpha" }).click();
  const syncsBefore = syncs;
  await page.keyboard.press("Control+e");
  await expect.poll(() => calls[0]).toEqual({ path: "/api/schedule", body: { uuids: ["a"], when: "evening" } });
  await expect(evening.locator(".task-title")).toHaveText(["Alpha", "Gamma"]);
  // Wait for the sync after the write, so the list is not redrawn mid-drag.
  await expect.poll(() => syncs).toBeGreaterThan(syncsBefore);
  await expect(evening.locator(".task-title")).toHaveText(["Alpha", "Gamma"]);

  // Out of This Evening: dropped above Beta, so Today again, placed first.
  await dragRow(page, "Gamma", "Beta");
  await expect.poll(() => calls[1]).toEqual({ path: "/api/schedule", body: { uuids: ["c"], when: "today" } });
  await expect.poll(() => calls[2]).toEqual({ path: "/api/reorder", body: { view: "today", moved: "c", order: ["c", "b"] } });
  await expect(evening.locator(".task-title")).toHaveText(["Alpha"]);

  // The editor's When menu names This Evening.
  await page.locator(".task-row", { hasText: "Alpha" }).dblclick();
  await expect(page.locator(".task-row.editing .edit-when")).toHaveValue("evening");
  await page.locator(".task-row.editing .edit-when").selectOption("today");
  await expect.poll(() => calls[3]).toEqual({ path: "/api/schedule", body: { uuids: ["a"], when: "today" } });
});

test("a failed reorder restores the order", async ({ page }) => {
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("a", "Alpha"), task("b", "Beta")]) }),
    "POST /api/reorder": (route) => route.fulfill({ status: 502, json: { error: "Things Cloud is unreachable." } }),
  });

  await page.goto("/");
  await dragRow(page, "Alpha", "Beta", { after: true });
  await expect(page.locator(".toast.error")).toContainText("Could not move “Alpha”");
  await expect(page.locator(".task-title")).toHaveText(["Alpha", "Beta"]);
});

test("new to-dos appear at the top", async ({ page }) => {
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("t1", "Existing")]) }),
    "POST /api/create": () => new Promise(() => {}),
  });
  await page.goto("/");
  await page.locator("#quick-add input[name=title]").fill("Brand new");
  await page.locator("#quick-add input[name=title]").press("Enter");
  await expect(page.locator(".task-title").first()).toHaveText("Brand new");
});

test("a sync that started before a create neither hides the new to-do nor ends the syncing", async ({ page }) => {
  const created = { ...task("t-new", "Brand new", ["today", "anytime"]) };
  const slow = deferred();
  let fresh = 0;
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("t1", "Existing")]) }),
    "GET /api/sync?fresh": async (route) => {
      fresh += 1;
      if (fresh === 1) {
        await slow.promise;
        return route.fulfill({ json: model([task("t1", "Existing")]) });
      }
      return route.fulfill({ json: model([created, task("t1", "Existing")]) });
    },
    "POST /api/create": (route) => route.fulfill({ json: { created: true, uuid: "t-new", task: created } }),
  });
  await page.goto("/");
  await expect(page.locator(".task-title")).toHaveText(["Existing"]);
  await page.locator("#refresh").click();
  await expect.poll(() => fresh).toBe(1);
  await page.locator("#quick-add input[name=title]").fill("Brand new");
  await page.locator("#quick-add input[name=title]").press("Enter");
  await expect(page.locator(".task-row:not(.pending) .task-title")).toHaveText(["Brand new", "Existing"]);
  // The old sync returns without the new to-do: it stays, and one more sync
  // runs for the change.
  slow.resolve();
  await expect.poll(() => fresh).toBe(2);
  await expect(page.locator(".task-title")).toHaveText(["Brand new", "Existing"]);
  await page.waitForTimeout(300);
  expect(fresh).toBe(2);
});

test("a to-do is edited in place: title, notes and checklist", async ({ page }) => {
  const calls = [];
  const record = (route, request) => {
    calls.push({ path: new URL(request.url()).pathname, body: request.postDataJSON() });
    return route.fulfill({ json: { ok: true, uuid: "item-new" } });
  };
  const withChecklist = { ...task("t1", "Plan trip"), note: "Old note", checklist: [{ uuid: "i1", title: "Book train", done: false }] };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([withChecklist]) }),
    "POST /api/update": record,
    "POST /api/checklist": record,
  });

  await page.goto("/");
  await page.locator(".task-row", { hasText: "Plan trip" }).dblclick();
  const editor = page.locator(".task-row.editing");
  await expect(editor.locator(".edit-title")).toBeFocused();
  await editor.locator(".edit-title").fill("Plan summer trip");
  await editor.locator(".edit-title").press("Enter");
  await expect(editor.locator(".edit-note")).toBeFocused();
  await editor.locator(".edit-note").fill("Old note\nPack light");
  await editor.locator(".item-new").fill("Book hotel");
  await editor.locator(".item-new").press("Enter");
  await editor.getByRole("button", { name: "Complete Book train" }).click();
  await page.keyboard.press("Escape");

  await expect(page.locator(".task-row.editing")).toHaveCount(0);
  await expect(page.locator(".task-title")).toHaveText(["Plan summer trip"]);
  await expect.poll(() => calls.length).toBeGreaterThanOrEqual(4);
  expect(calls).toContainEqual({ path: "/api/update", body: { uuid: "t1", title: "Plan summer trip" } });
  expect(calls).toContainEqual({ path: "/api/update", body: { uuid: "t1", note: "Old note\nPack light" } });
  expect(calls).toContainEqual({ path: "/api/checklist", body: { action: "add", task: "t1", title: "Book hotel" } });
  expect(calls).toContainEqual({ path: "/api/checklist", body: { action: "complete", task: "t1", uuid: "i1", done: true } });
});

test("selected to-dos are deleted with the Delete key and can be restored", async ({ page }) => {
  const calls = [];
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("a", "Alpha"), task("b", "Beta"), task("c", "Gamma")]) }),
    "POST /api/trash": (route, request) => {
      calls.push(["trash", request.postDataJSON().uuids]);
      return route.fulfill({ json: { ok: true } });
    },
    "POST /api/restore": (route, request) => {
      calls.push(["restore", request.postDataJSON().uuids]);
      return route.fulfill({ json: { ok: true } });
    },
  });

  await page.goto("/");
  await page.locator(".task-row", { hasText: "Alpha" }).click();
  await expect(page.locator(".task-row.selected")).toHaveCount(1);
  await page.locator(".task-row", { hasText: "Beta" }).click({ modifiers: ["Shift"] });
  await expect(page.locator(".task-row.selected")).toHaveCount(2);
  await page.keyboard.press("Delete");
  await expect(page.locator(".task-title")).toHaveText(["Gamma"]);
  await expect.poll(() => calls[0]).toEqual(["trash", ["a", "b"]]);
  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => calls[1]).toEqual(["restore", ["a", "b"]]);
  await expect(page.locator(".task-title")).toHaveText(["Alpha", "Beta", "Gamma"]);
});

test("arrow keys select, Enter opens, Ctrl+K completes", async ({ page }) => {
  let completed = "";
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("a", "Alpha"), task("b", "Beta")]) }),
    "POST /api/complete": (route, request) => {
      completed = request.postDataJSON().uuid;
      return route.fulfill({ json: { completed: true } });
    },
  });
  await page.goto("/");
  await expect(page.locator(".task-title")).toHaveText(["Alpha", "Beta"]);
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(page.locator(".task-row.selected")).toContainText("Beta");
  await page.keyboard.press("Control+k");
  await expect.poll(() => completed).toBe("b");
  await page.keyboard.press("Enter");
  await expect(page.locator(".task-row.editing .edit-title")).toHaveValue("Beta");
});

test("projects: progress pie, Complete Project asks about open to-dos, header sets when, deadline and tags", async ({ page }) => {
  const calls = [];
  const inHouse = (uuid, title, status = "open") => ({ ...task(uuid, title, ["anytime"]), project: "p-house", projectTitle: "House", status });
  const record = (route, request) => {
    const body = request.postDataJSON();
    calls.push({ path: new URL(request.url()).pathname, body });
    // The server keeps what was written, so later syncs show it.
    const project = data.projects.find((item) => item.uuid === (body.uuid || body.uuids?.[0]));
    if (project && body.when) project.when = body.when === "someday" ? "someday" : "anytime";
    if (project && "deadline" in body) project.deadline = body.deadline;
    if (project && body.tags) {
      project.tagIDs = body.tags;
      project.tags = body.tags.map((id) => data.tags.find((tag) => tag.uuid === id).title);
    }
    return route.fulfill({ json: { ok: true } });
  };
  const data = model([inHouse("a", "Paint"), inHouse("b", "Sink"), inHouse("c", "Plan", "completed")], {
    areas: [],
    tags: [{ uuid: "tag-errand", title: "Errand" }],
    projects: [
      { uuid: "p-house", title: "House", index: 1, area: "", when: "anytime", status: "open", done: 1, total: 4, deadline: "", tags: [], tagIDs: [], count: 2 },
      { uuid: "p-garden", title: "Garden", index: 2, area: "", when: "anytime", status: "completed", done: 2, total: 2, count: 0 },
    ],
  });
  const finish = (route, request) => {
    // The server keeps what was written, so later syncs show it.
    const { uuid, mode, rest } = request.postDataJSON();
    data.projects.find((item) => item.uuid === uuid).status = mode === "complete" ? "completed" : "canceled";
    for (const item of data.tasks) if (item.project === uuid && item.status === "open") item.status = rest === "complete" ? "completed" : "canceled";
    return record(route, request);
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/project-complete": finish,
    "POST /api/schedule": record,
    "POST /api/deadline": record,
    "POST /api/tags": record,
  });

  await page.goto("/");
  const house = page.locator(".mini-item.project-row", { hasText: "House" });
  await expect(house.locator(".pie")).toHaveAttribute("data-progress", "1/4");
  // A completed project stays, crossed out, until it is logged.
  await expect(page.locator(".mini-item.project-row.finished", { hasText: "Garden" }).locator(".pie")).toHaveAttribute("data-progress", "completed");

  await house.click();
  await expect(page.locator("#view-title")).toHaveText("House");
  const details = page.locator("#project-details");
  await details.locator(".project-when").selectOption("someday");
  await expect.poll(() => calls[0]).toEqual({ path: "/api/schedule", body: { uuids: ["p-house"], when: "someday" } });
  // A Someday project leaves the sidebar, as in Things; bring it back.
  await expect(house).toHaveCount(0);
  await details.locator(".project-when").selectOption("anytime");
  await expect.poll(() => calls[1]).toEqual({ path: "/api/schedule", body: { uuids: ["p-house"], when: "anytime" } });
  await expect(house).toHaveCount(1);
  await details.locator(".project-deadline").fill("2099-05-06");
  await expect.poll(() => calls[2]).toEqual({ path: "/api/deadline", body: { uuid: "p-house", deadline: "2099-05-06" } });
  await details.locator(".project-tag-new").fill("Errand");
  await details.locator(".project-tag-new").press("Enter");
  await expect.poll(() => calls[3]).toEqual({ path: "/api/tags", body: { uuid: "p-house", tags: ["tag-errand"] } });
  await expect(details.locator(".pill.tag")).toHaveText(["Errand×"]);

  // Two to-dos are still open: Things asks whether to complete or cancel them.
  await page.locator("#complete-project").click();
  await expect(page.locator("#project-finish")).toBeVisible();
  await expect(page.locator("#project-finish-text")).toContainText("2 open to-dos");
  await page.locator("#project-finish").getByRole("button", { name: "Back" }).click();
  await expect(page.locator("#project-finish")).toBeHidden();
  expect(calls.length).toBe(4);

  await house.click({ button: "right" });
  await page.locator(".context-menu").getByRole("menuitem", { name: "Complete Project…" }).click();
  await page.locator("#project-finish").getByRole("button", { name: "Cancel Them" }).click();
  await expect.poll(() => calls[4]).toEqual({ path: "/api/project-complete", body: { uuid: "p-house", mode: "complete", rest: "cancel" } });
  await expect(page.locator(".task-row.canceled .task-title")).toHaveText(["Paint", "Sink"]);
  await expect(house.locator(".pie")).toHaveAttribute("data-progress", "completed");
  await expect(page.locator("#complete-project")).toBeHidden();
});

test("projects are reordered in the sidebar by dragging", async ({ page }) => {
  let request = null;
  const data = model([], {
    areas: [{ uuid: "a-private", title: "Private", index: 1, count: 0 }],
    projects: [
      { uuid: "p-trip", title: "City Trip", index: -90, area: "a-private", when: "anytime" },
      { uuid: "p-move", title: "Relocation", index: -50, area: "a-private", when: "anytime" },
      { uuid: "p-bike", title: "Bike", index: 20, area: "a-private", when: "anytime" },
    ],
  });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/reorder": (route, req) => {
      request = req.postDataJSON();
      // Like the server, the sync after the write shows the new order.
      for (const project of data.projects) project.index = request.order.indexOf(project.uuid);
      return route.fulfill({ json: { reordered: true } });
    },
  });
  await page.goto("/");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Private", "City Trip", "Relocation", "Bike"]);
  const from = await page.locator("#projects .mini-item", { hasText: "Bike" }).boundingBox();
  const to = await page.locator("#projects .mini-item", { hasText: "City Trip" }).boundingBox();
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height * 0.2, { steps: 8 });
  await page.mouse.up();
  await expect(page.locator("#projects .mini-title")).toHaveText(["Private", "Bike", "City Trip", "Relocation"]);
  await expect.poll(() => request).toEqual({ view: "projects", moved: "p-bike", order: ["p-bike", "p-trip", "p-move"] });
  // The drag did not open the project.
  await expect(page.locator("#view-title")).toHaveText("Today");
});

test("a project is deleted from the sidebar with its to-dos and can be restored", async ({ page }) => {
  const calls = [];
  const box = { ...task("t-box", "Buy boxes", ["anytime"]), project: "p-move", projectTitle: "Relocation" };
  const data = model([box, task("t-other", "Other")], {
    projects: [
      { uuid: "p-move", title: "Relocation", index: 1, area: "", when: "anytime", count: 1 },
      { uuid: "p-bike", title: "Bike", index: 2, area: "", when: "anytime", count: 0 },
    ],
  });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/trash": (route, request) => {
      calls.push(["trash", request.postDataJSON().uuids]);
      return route.fulfill({ json: { ok: true } });
    },
    "POST /api/restore": (route, request) => {
      calls.push(["restore", request.postDataJSON().uuids]);
      return route.fulfill({ json: { ok: true } });
    },
  });
  const dialogs = [];
  page.on("dialog", (dialog) => {
    dialogs.push(dialog.message());
    return dialogs.length === 1 ? dialog.dismiss() : dialog.accept();
  });
  await page.goto("/");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Relocation", "Bike"]);

  // Right click offers Delete Project; cancelling the question keeps it.
  await page.locator("#projects .mini-item", { hasText: "Bike" }).click({ button: "right" });
  await page.locator(".context-menu").getByRole("menuitem", { name: "Delete Project…" }).click();
  await expect.poll(() => dialogs.length).toBe(1);
  await expect(page.locator("#projects .mini-title")).toHaveText(["Relocation", "Bike"]);
  expect(calls).toEqual([]);

  // Selected in the sidebar, Delete moves it and its to-dos to the Trash.
  await page.locator("#projects .mini-item", { hasText: "Relocation" }).click();
  await expect(page.locator("#view-title")).toHaveText("Relocation");
  await expect(page.locator("#delete-project")).toBeVisible();
  await page.keyboard.press("Delete");
  await expect.poll(() => dialogs[1]).toBe("Move the project “Relocation” and its 1 open to-do to the Trash?");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Bike"]);
  await expect(page.locator("#view-title")).toHaveText("Inbox");
  await expect.poll(() => calls[0]).toEqual(["trash", ["p-move"]]);
  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => calls[1]).toEqual(["restore", ["p-move"]]);
  await expect(page.locator("#projects .mini-title")).toHaveText(["Relocation", "Bike"]);
});

test("projects and areas are renamed in the header and the sidebar; project notes are edited", async ({ page }) => {
  const calls = [];
  const box = { ...task("t-box", "Buy boxes", ["anytime"]), project: "p-move", projectTitle: "Relocation" };
  const data = model([box], {
    areas: [{ uuid: "a-home", title: "Home", index: 1, count: 0 }],
    projects: [{ uuid: "p-move", title: "Relocation", note: "", index: 1, area: "a-home", when: "anytime", count: 1 }],
  });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    // Like the server, later syncs show the change.
    "POST /api/update": (route, request) => {
      const body = request.postDataJSON();
      calls.push(["update", body]);
      Object.assign(data.projects[0], body.title ? { title: body.title } : {}, "note" in body ? { note: body.note } : {});
      return route.fulfill({ json: { updated: true } });
    },
    "POST /api/area-rename": (route, request) => {
      calls.push(["area", request.postDataJSON()]);
      data.areas[0].title = request.postDataJSON().title;
      return route.fulfill({ json: { ok: true } });
    },
  });
  await page.goto("/");
  await page.locator("#projects .mini-item", { hasText: "Relocation" }).click();
  await expect(page.locator("#view-title")).toHaveText("Relocation");

  // Header: click the title, type, Enter.
  await page.locator("#view-title").click();
  await page.locator("#view-title input").fill("Move to Berlin");
  await page.keyboard.press("Enter");
  await expect(page.locator("#view-title")).toHaveText("Move to Berlin");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Home", "Move to Berlin"]);
  await expect.poll(() => calls[0]).toEqual(["update", { uuid: "p-move", title: "Move to Berlin" }]);

  // Notes below the title.
  await page.locator("#project-note").fill("Boxes first");
  await page.locator("#view-title").focus();
  await page.locator(".content").click({ position: { x: 5, y: 5 } });
  await expect.poll(() => calls[1]).toEqual(["update", { uuid: "p-move", note: "Boxes first" }]);

  // Sidebar: right click, Rename…, Escape cancels; double click renames.
  await page.locator("#projects .mini-item", { hasText: "Home" }).click({ button: "right" });
  await page.locator(".context-menu").getByRole("menuitem", { name: "Rename…" }).click();
  await page.locator("#projects .rename-input").fill("Nope");
  await page.keyboard.press("Escape");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Home", "Move to Berlin"]);
  await page.locator("#projects .mini-item", { hasText: "Home" }).dblclick();
  await page.locator("#projects .rename-input").fill("Family");
  await page.keyboard.press("Enter");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Family", "Move to Berlin"]);
  await expect.poll(() => calls[2]).toEqual(["area", { uuid: "a-home", title: "Family" }]);
  expect(calls).toHaveLength(3);
});

test("a new to-do can be opened and given a checklist right after adding it", async ({ page }) => {
  const checklist = [];
  const release = deferred();
  let created = null;
  await mockApp(page, {
    "GET /api/status": signedIn,
    // Like the server, later syncs include the new to-do.
    "GET /api/sync": (route) => route.fulfill({ json: model(created ? [created, task("t1", "Existing")] : [task("t1", "Existing")]) }),
    "POST /api/create": async (route, request) => {
      await release.promise;
      const body = request.postDataJSON();
      created = { ...task("new-1", body.title), checklist: [] };
      await route.fulfill({ json: { created: true, uuid: "new-1", task: created } });
    },
    "POST /api/checklist": (route, request) => {
      checklist.push(request.postDataJSON());
      return route.fulfill({ json: { ok: true, uuid: "item-1" } });
    },
  });
  await page.goto("/");
  await page.locator("#quick-add input[name=title]").fill("Pack bag");
  await page.locator("#quick-add input[name=title]").press("Enter");
  // Double click while it is still being created: it opens once it exists.
  await page.locator(".task-row.pending").dblclick();
  release.resolve();
  const editor = page.locator(".task-row.editing");
  await expect(editor.locator(".edit-title")).toHaveValue("Pack bag");
  await editor.locator(".item-new").fill("Passport");
  await editor.locator(".item-new").press("Enter");
  await expect.poll(() => checklist[0]).toEqual({ action: "add", task: "new-1", title: "Passport" });
  await expect(editor.locator(".item-title")).toHaveValue("Passport");
});

test("browser errors are reported to the server log", async ({ page }) => {
  const reports = [];
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([]) }),
    "POST /api/client-error": (route, request) => {
      reports.push(request.postDataJSON());
      return route.fulfill({ status: 204 });
    },
  });
  await page.goto("/");
  await page.evaluate(() => setTimeout(() => { throw new Error("boom from test"); }, 0));
  await expect.poll(() => reports.length).toBe(1);
  expect(reports[0]).toMatchObject({ message: expect.stringContaining("boom from test"), context: "error" });
});

test("selected to-dos are dragged into another project", async ({ page }) => {
  let request = null;
  const inProject = (id, title) => ({ ...task(id, title, ["anytime"]), project: "p-haus", projectTitle: "Old House" });
  const data = model([inProject("a", "Kisten"), inProject("b", "Strom"), inProject("c", "Wasser"), inProject("d", "Bleibt")], {
    areas: [],
    projects: [
      { uuid: "p-haus", title: "Old House", index: 1, area: "", when: "anytime" },
      { uuid: "p-move", title: "Relocation", index: 2, area: "", when: "anytime" },
    ],
  });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/move": (route, req) => {
      request = req.postDataJSON();
      // Like the server, later syncs show the move.
      for (const item of data.tasks) if (request.uuids.includes(item.uuid)) Object.assign(item, { project: request.project, projectTitle: "Relocation" });
      return route.fulfill({ json: { moved: 3 } });
    },
  });
  await page.goto("/");
  await page.locator("#projects .mini-item", { hasText: "Old House" }).click();
  await page.locator(".task-row", { hasText: "Kisten" }).click();
  await page.locator(".task-row", { hasText: "Wasser" }).click({ modifiers: ["Shift"] });
  await expect(page.locator(".task-row.selected")).toHaveCount(3);
  // Shift+click must not select text: dragging selected text would start the
  // browser's own drag and cancel the move.
  expect(await page.evaluate(() => window.getSelection().toString())).toBe("");
  expect(await page.evaluate(() => {
    const event = new DragEvent("dragstart", { bubbles: true, cancelable: true });
    document.querySelector(".task-row .task-title").dispatchEvent(event);
    return event.defaultPrevented;
  })).toBe(true);

  const from = await page.locator(".task-row", { hasText: "Strom" }).boundingBox();
  const to = await page.locator("#projects .mini-item", { hasText: "Relocation" }).boundingBox();
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 });
  await expect(page.locator(".drag-ghost")).toHaveText("3 to-dos");
  await page.mouse.up();

  await expect.poll(() => request).toEqual({ uuids: ["a", "b", "c"], project: "p-move" });
  await expect(page.locator(".task-title")).toHaveText(["Bleibt"]);
  await expect(page.locator(".toast.success")).toHaveText("Moved 3 to-dos to Relocation");
  await expect(page.locator(".dragging")).toHaveCount(0);
});

test("the editor sets when, deadline and tags", async ({ page }) => {
  const calls = [];
  let views = ["today", "anytime"];
  const record = (route, request) => {
    const body = request.postDataJSON();
    calls.push({ path: new URL(request.url()).pathname, body });
    if (body.when === "someday") views = ["someday"];
    return route.fulfill({ json: { ok: true, uuid: "tag-new", title: "Garden" } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    // Like the server, later syncs show the new schedule.
    "GET /api/sync": (route) =>
      route.fulfill({ json: model([{ ...task("t1", "Plan", views), tagIDs: [], checklist: [] }], { tags: [{ uuid: "tag-errand", title: "Errand" }] }) }),
    "POST /api/schedule": record,
    "POST /api/deadline": record,
    "POST /api/tags": record,
    "POST /api/tag-create": record,
  });
  await page.goto("/");
  await page.locator(".task-row", { hasText: "Plan" }).dblclick();
  const editor = page.locator(".task-row.editing");
  await expect(editor.locator(".edit-when")).toHaveValue("today");
  // The footer sits below title, notes and checklist, inside the editor.
  await expect(editor.locator(".task-editor > .editor-footer")).toHaveCount(1);
  await editor.locator(".edit-when").selectOption("someday");
  await editor.locator(".edit-deadline").fill("2030-02-03");
  await editor.locator(".tag-new").fill("errand");
  await editor.locator(".tag-new").press("Enter");
  await expect(editor.locator(".pill.tag")).toContainText("Errand");
  await editor.locator(".tag-new").fill("Garden");
  await editor.locator(".tag-new").press("Enter");
  await expect(editor.locator(".pill.tag")).toHaveCount(2);
  await editor.getByRole("button", { name: "Remove tag Errand" }).click();
  await expect(editor.locator(".pill.tag")).toHaveCount(1);

  expect(calls).toContainEqual({ path: "/api/schedule", body: { uuids: ["t1"], when: "someday" } });
  expect(calls).toContainEqual({ path: "/api/deadline", body: { uuid: "t1", deadline: "2030-02-03" } });
  expect(calls).toContainEqual({ path: "/api/tags", body: { uuid: "t1", tags: ["tag-errand"] } });
  expect(calls).toContainEqual({ path: "/api/tag-create", body: { title: "Garden" } });
  expect(calls).toContainEqual({ path: "/api/tags", body: { uuid: "t1", tags: ["tag-errand", "tag-new"] } });
  await expect.poll(() => calls.at(-1)).toEqual({ path: "/api/tags", body: { uuid: "t1", tags: ["tag-new"] } });
  // Closing the editor shows that the to-do left Today.
  await page.keyboard.press("Escape");
  await expect(page.locator(".task-title", { hasText: "Plan" })).toBeHidden();
});

test("Repeat… turns a to-do into a repeating one; Undo brings it back", async ({ page }) => {
  const calls = [];
  const original = { ...task("t1", "Water plants", ["anytime"]), repeat: "" };
  const template = { ...task("tpl", "Water plants", ["upcoming"]), repeat: "Every 2 weeks on Thursday" };
  let tasks = [original];
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    calls.push({ path, body });
    if (path === "/api/repeat") {
      tasks = [template];
      return route.fulfill({ json: { ok: true, uuid: "tpl" } });
    }
    if (path === "/api/restore") tasks = [original, template];
    if (path === "/api/trash") tasks = [original];
    return route.fulfill({ json: { ok: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model(tasks) }),
    "POST /api/repeat": record,
    "POST /api/restore": record,
    "POST /api/trash": record,
  });
  await page.goto("/");
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  await page.locator(".task-row", { hasText: "Water plants" }).dblclick();
  await page.locator(".task-row.editing").getByRole("button", { name: "Repeat…" }).click();
  const dialog = page.locator("#repeat-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.locator("#repeat-month")).toBeHidden();
  await dialog.locator("select[name=unit]").selectOption("month");
  await expect(dialog.locator("#repeat-month")).toBeVisible();
  await dialog.locator("select[name=unit]").selectOption("week");
  await dialog.locator("input[name=every]").fill("2");
  await dialog.locator("input[name=start]").fill("2030-05-02");
  await dialog.getByRole("button", { name: "Repeat", exact: true }).click();
  await expect.poll(() => calls[0]).toEqual({ path: "/api/repeat", body: { uuid: "t1", type: "fixed", unit: "week", every: 2, start: "2030-05-02" } });
  await expect(page.locator(".task-title", { hasText: "Water plants" })).toBeHidden();

  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => calls.slice(1)).toEqual([
    { path: "/api/restore", body: { uuids: ["t1"] } },
    { path: "/api/trash", body: { uuids: ["tpl"] } },
  ]);
  await expect(page.locator(".task-title", { hasText: "Water plants" })).toBeVisible();

  // Mod+Shift+R opens the dialog for the selected to-do; Cancel writes nothing.
  await page.locator(".task-row", { hasText: "Water plants" }).click();
  await page.keyboard.press("Control+Shift+R");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  expect(calls).toHaveLength(3);
});

test("the tag bar filters a list; the Tags dialog renames and deletes tags", async ({ page }) => {
  const calls = [];
  const tagged = (id, title, tags) => ({ ...task(id, title, ["anytime"]), tagIDs: tags.map((tag) => tag.uuid), tags: tags.map((tag) => tag.title) });
  const errand = { uuid: "tag-errand", title: "Errand" };
  const home = { uuid: "tag-home", title: "Home" };
  const data = model([tagged("a", "Buy milk", [errand]), tagged("b", "Fix door", [home]), tagged("c", "Read", [])], { tags: [errand, home] });
  // Like the server, later syncs show the changes.
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    calls.push({ path, body });
    if (path === "/api/tag-rename") data.tags.find((tag) => tag.uuid === body.uuid).title = body.title;
    if (path === "/api/tag-delete") data.tags = data.tags.filter((tag) => tag.uuid !== body.uuid);
    for (const item of data.tasks) {
      item.tagIDs = item.tagIDs.filter((id) => data.tags.some((tag) => tag.uuid === id));
      item.tags = item.tagIDs.map((id) => data.tags.find((tag) => tag.uuid === id).title);
    }
    return route.fulfill({ json: { ok: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/tag-rename": record,
    "POST /api/tag-delete": record,
  });
  await page.goto("/");
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  const bar = page.locator(".tag-filter");
  await expect(bar.locator("[data-tag-filter]")).toHaveText(["Errand", "Home"]);
  await bar.getByRole("button", { name: "Errand" }).click();
  await expect(page.locator(".task-title")).toHaveText(["Buy milk"]);
  await bar.getByRole("button", { name: "Errand" }).click();
  await expect(page.locator(".task-title")).toHaveText(["Buy milk", "Fix door", "Read"]);
  // The filter belongs to its list.
  await bar.getByRole("button", { name: "Home" }).click();
  await page.locator(".nav-item", { hasText: "Inbox" }).click();
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  await expect(page.locator(".task-title")).toHaveText(["Fix door"]);
  await bar.getByRole("button", { name: "Home" }).click();

  await page.getByRole("button", { name: "Tags", exact: true }).click();
  const dialog = page.locator("#tags-dialog");
  await dialog.getByRole("textbox", { name: "Name of Errand" }).fill("Shop");
  await dialog.getByRole("textbox", { name: "Name of Errand" }).press("Enter");
  await expect.poll(() => calls[0]).toEqual({ path: "/api/tag-rename", body: { uuid: "tag-errand", title: "Shop" } });
  await expect(page.locator(".task-row", { hasText: "Buy milk" }).locator(".pill.tag")).toHaveText("Shop");

  page.once("dialog", (question) => {
    expect(question.message()).toBe("Delete the tag “Home”? It is removed from 1 item.");
    return question.accept();
  });
  await dialog.getByRole("button", { name: "Delete Home" }).click();
  await expect.poll(() => calls[1]).toEqual({ path: "/api/tag-delete", body: { uuid: "tag-home" } });
  await expect(dialog.locator("[data-tag-rename]")).toHaveCount(1);
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(bar.locator("[data-tag-filter]")).toHaveText(["Shop"]);
  await expect(page.locator(".task-row", { hasText: "Fix door" }).locator(".pill.tag")).toHaveCount(0);
});

test("areas carry tags: the area header sets them, their to-dos match them in the tag bar", async ({ page }) => {
  const calls = [];
  const work = { uuid: "tag-work", title: "Work" };
  const errand = { uuid: "tag-errand", title: "Errand" };
  const area = { uuid: "a-office", title: "Office", type: "area", index: 0, count: 1, tags: [work.title], tagIDs: [work.uuid] };
  const inArea = { ...task("t1", "File taxes", ["anytime"]), area: area.uuid, areaTitle: area.title };
  const data = model([inArea, task("t2", "Buy milk", ["anytime"])], { areas: [area], tags: [work, errand] });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/tags": (route, request) => {
      const body = request.postDataJSON();
      calls.push(body);
      area.tagIDs = body.tags;
      area.tags = body.tags.map((id) => data.tags.find((tag) => tag.uuid === id).title);
      return route.fulfill({ json: { ok: true } });
    },
  });
  await page.goto("/");
  // The to-do has no tag of its own; it matches the tag of its area.
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  const bar = page.locator(".tag-filter");
  await expect(bar.locator("[data-tag-filter]")).toHaveText(["Work"]);
  await bar.getByRole("button", { name: "Work" }).click();
  await expect(page.locator(".task-title")).toHaveText(["File taxes"]);

  await page.locator(`.area-row[data-scope-id="a-office"]`).click();
  const details = page.locator("#project-details");
  await expect(details.locator(".pill.tag")).toHaveText(["Work×"]);
  await expect(details.locator(".project-when")).toHaveCount(0);
  await details.getByRole("combobox", { name: "Add area tag" }).fill("Errand");
  await details.getByRole("combobox", { name: "Add area tag" }).press("Enter");
  await expect.poll(() => calls[0]).toEqual({ uuid: "a-office", tags: ["tag-work", "tag-errand"] });
  await expect(details.locator(".pill.tag")).toHaveText(["Work×", "Errand×"]);
  await details.getByRole("button", { name: "Remove tag Work" }).click();
  await expect.poll(() => calls[1]).toEqual({ uuid: "a-office", tags: ["tag-errand"] });
  await expect(details.locator(".pill.tag")).toHaveText(["Errand×"]);
});

test("Convert to Project… makes a project of a to-do and opens it", async ({ page }) => {
  const calls = [];
  const original = { ...task("t1", "Move house", ["anytime"]), checklist: [{ uuid: "c1", title: "Boxes", done: false, index: 0 }] };
  const data = model([original], { projects: [] });
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    calls.push({ path, body: request.postDataJSON() });
    if (path === "/api/convert-project") {
      data.projects = [{ uuid: "p-new", title: "Move house", index: 0, area: "", when: "anytime", count: 1 }];
      data.tasks = [{ ...task("t2", "Boxes", ["anytime"]), project: "p-new", projectTitle: "Move house" }];
      return route.fulfill({ json: { ok: true, uuid: "p-new", todos: 1 } });
    }
    data.tasks.push(original);
    return route.fulfill({ json: { ok: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/convert-project": record,
    "POST /api/restore": record,
  });
  const dialogs = [];
  page.on("dialog", (dialog) => {
    dialogs.push(dialog.message());
    return dialog.accept();
  });
  await page.goto("/");
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  await page.locator(".task-row", { hasText: "Move house" }).dblclick();
  await page.locator(".task-row.editing").getByRole("button", { name: "Convert to Project…" }).click();
  await expect.poll(() => dialogs[0]).toBe("Convert “Move house” to a project? Its checklist item becomes a to-do.");
  await expect.poll(() => calls[0]).toEqual({ path: "/api/convert-project", body: { uuid: "t1" } });
  await expect(page.locator("#view-title")).toHaveText("Move house");
  await expect(page.locator(".task-title")).toHaveText(["Boxes"]);
  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => calls[1]).toEqual({ path: "/api/restore", body: { uuids: ["t1"] } });
  await expect(page.locator(".toast").last()).toContainText("the project stays");
});

test("search also finds logged to-dos; the Logbook shows more on request", async ({ page }) => {
  const searches = [];
  const limits = [];
  const logged = (i) => ({ uuid: `l${i}`, title: `Logged ${i}`, type: "task", status: "completed", completedAt: "2026-01-05T10:00:00Z" });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([task("t1", "Mow the lawn", ["today"])]) }),
    "GET /api/search": (route, request) => {
      searches.push(new URL(request.url()).searchParams.get("q"));
      return route.fulfill({ json: { total: 1, items: [{ uuid: "l9", title: "Mow last summer", type: "task", status: "completed", completedAt: "2025-07-01T10:00:00Z" }] } });
    },
    "GET /api/logbook": (route, request) => {
      const limit = Number(new URL(request.url()).searchParams.get("limit"));
      limits.push(limit);
      return route.fulfill({ json: { total: 2, items: limit > 300 ? [logged(1), logged(2)] : [logged(1)] } });
    },
  });
  await page.goto("/");
  await page.locator("#search").fill("mow");
  await expect(page.locator(".search-logbook h2")).toHaveText("Logbook");
  await expect(page.locator(".task-title")).toHaveText(["Mow the lawn", "Mow last summer"]);
  // One request after typing stopped.
  expect(searches).toEqual(["mow"]);
  await page.locator("#search").fill("");
  await expect(page.locator(".search-logbook")).toHaveCount(0);

  await page.locator(".nav-item", { hasText: "Logbook" }).click();
  await expect(page.locator(".task-title")).toHaveText(["Logged 1"]);
  await page.getByRole("button", { name: "Show more" }).click();
  await expect(page.locator(".task-title")).toHaveText(["Logged 1", "Logged 2"]);
  expect(limits).toEqual([300, 600]);
  await expect(page.getByRole("button", { name: "Show more" })).toHaveCount(0);
});

test("notes show their Markdown safely; Ctrl+B and Ctrl+I wrap the selection", async ({ page }) => {
  const notes = [];
  const note = "# Plan\n**Boxes** and *tape*, see [the list](https://example.com/list)\n- one\n- two <script>window.hacked = 1</script>";
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([{ ...task("t1", "Move", ["today"]), note }]) }),
    "POST /api/update": (route, request) => {
      notes.push(request.postDataJSON().note);
      return route.fulfill({ json: { updated: true } });
    },
  });
  await page.goto("/");
  await page.locator(".task-row", { hasText: "Move" }).dblclick();
  const preview = page.locator(".task-row.editing .note-markdown");
  await expect(preview.locator(".md-h1")).toHaveText("Plan");
  await expect(preview.locator("strong")).toHaveText("Boxes");
  await expect(preview.locator("em")).toHaveText("tape");
  await expect(preview.getByRole("link", { name: "the list" })).toHaveAttribute("href", "https://example.com/list");
  await expect(preview.locator("li")).toHaveText(["one", "two <script>window.hacked = 1</script>"]);
  await expect(preview.locator("script")).toHaveCount(0);
  expect(await page.evaluate(() => window.hacked)).toBeUndefined();

  const field = page.locator(".task-row.editing .edit-note");
  await field.fill("pack books");
  await field.evaluate((element) => element.setSelectionRange(5, 10));
  await page.keyboard.press("Control+B");
  await expect(field).toHaveValue("pack **books**");
  await field.evaluate((element) => element.setSelectionRange(0, 4));
  await page.keyboard.press("Control+I");
  await expect(field).toHaveValue("*pack* **books**");
  await expect(page.locator(".task-row.editing .note-markdown em")).toHaveText("pack");
  await expect.poll(() => notes.at(-1)).toBe("*pack* **books**");
});

test("areas fold in the sidebar, stay folded after a reload and still take dropped to-dos", async ({ page }) => {
  let request = null;
  const data = model([task("t1", "Plan the week", ["anytime"])], {
    areas: [
      { uuid: "a-home", title: "Home", index: 1, count: 3 },
      { uuid: "a-work", title: "Work", index: 2, count: 0 },
    ],
    projects: [
      { uuid: "p-garden", title: "Garden", index: 1, area: "a-home", when: "anytime" },
      { uuid: "p-launch", title: "Launch", index: 2, area: "a-work", when: "anytime" },
    ],
  });
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/move": (route, req) => {
      request = req.postDataJSON();
      return route.fulfill({ json: { moved: 1 } });
    },
  });
  await page.goto("/");
  const titles = page.locator("#projects .mini-title");
  await expect(titles).toHaveText(["Home", "Garden", "Work", "Launch"]);

  // The triangle folds; the folded area shows its open to-dos.
  const home = page.locator("#projects .area-row", { hasText: "Home" });
  await home.locator("[data-toggle-area]").click();
  await expect(titles).toHaveText(["Home", "Work", "Launch"]);
  await expect(home).toHaveAttribute("aria-expanded", "false");
  await expect(home.locator(".count")).toHaveText("3");
  await expect(page.locator("#view-title")).not.toHaveText("Home");

  // Folded after a reload; Right opens, Left folds a focused area.
  await page.reload();
  await expect(titles).toHaveText(["Home", "Work", "Launch"]);
  await page.locator("#projects .area-row", { hasText: "Work" }).focus();
  await page.keyboard.press("ArrowLeft");
  await expect(titles).toHaveText(["Home", "Work"]);
  await page.keyboard.press("ArrowRight");
  await expect(titles).toHaveText(["Home", "Work", "Launch"]);

  // A to-do dropped on the folded area moves into it.
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  const from = await page.locator(".task-row", { hasText: "Plan the week" }).boundingBox();
  const to = await page.locator("#projects .area-row", { hasText: "Home" }).boundingBox();
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 });
  await page.mouse.up();
  await expect.poll(() => request).toEqual({ uuids: ["t1"], area: "a-home" });

  // Resting on a folded area while dragging opens it.
  await page.locator("#projects .area-row", { hasText: "Home" }).locator("[data-toggle-area]").click();
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 });
  await expect(titles).toHaveText(["Home", "Garden", "Work", "Launch"]);
  await page.mouse.up();
});

test("the editor sets and removes a reminder, only with a date", async ({ page }) => {
  const calls = [];
  const rows = { t1: { ...task("t1", "Call", ["today"]), reminder: "" }, t2: { ...task("t2", "Read", ["anytime"]), reminder: "" } };
  await mockApp(page, {
    "GET /api/status": signedIn,
    // Like the server, later syncs show the reminder.
    "GET /api/sync": (route) => route.fulfill({ json: model(Object.values(rows)) }),
    "POST /api/reminder": (route, request) => {
      const body = request.postDataJSON();
      calls.push(body);
      rows[body.uuid].reminder = body.time;
      return route.fulfill({ json: { ok: true } });
    },
  });
  await page.goto("/");
  await page.locator(".task-row", { hasText: "Call" }).dblclick();
  const editor = page.locator(".task-row.editing");
  await editor.locator(".edit-reminder").fill("12:30");
  await editor.locator(".edit-reminder").blur();
  await expect.poll(() => calls[0]).toEqual({ uuid: "t1", time: "12:30" });
  await page.keyboard.press("Escape");
  await expect(page.locator(".task-row", { hasText: "Call" }).locator(".reminder")).toHaveText("12:30");

  await page.locator(".task-row", { hasText: "Call" }).dblclick();
  await page.locator(".task-row.editing").getByRole("button", { name: "Remove reminder" }).click();
  await expect.poll(() => calls[1]).toEqual({ uuid: "t1", time: "" });
  await page.keyboard.press("Escape");
  await expect(page.locator(".task-row", { hasText: "Call" }).locator(".reminder")).toHaveCount(0);

  // Anytime has no date, so the reminder field is off.
  await page.locator(".nav-item", { hasText: "Anytime" }).click();
  await page.locator(".task-row", { hasText: "Read" }).dblclick();
  await expect(page.locator(".task-row.editing .edit-reminder")).toBeDisabled();
});

test("the Logbook reopens and the Trash restores to-dos", async ({ page }) => {
  const calls = [];
  const now = new Date();
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([]) }),
    "GET /api/logbook": (route) =>
      route.fulfill({
        json: {
          total: 2,
          items: [
            { uuid: "l1", title: "Filed taxes", type: "task", status: "completed", completedAt: now.toISOString(), projectTitle: "Admin" },
            { uuid: "l2", title: "Old project", type: "project", status: "completed", completedAt: "2025-01-02T10:00:00Z" },
          ],
        },
      }),
    "GET /api/trash": (route) => route.fulfill({ json: { total: 1, items: [{ uuid: "t9", title: "Bad idea", type: "task", status: "open" }] } }),
    "POST /api/reopen": (route, request) => {
      calls.push(["reopen", request.postDataJSON()]);
      return route.fulfill({ json: { ok: true } });
    },
    "POST /api/restore": (route, request) => {
      calls.push(["restore", request.postDataJSON()]);
      return route.fulfill({ json: { ok: true } });
    },
  });
  await page.goto("/");
  await page.locator(".nav-item", { hasText: "Logbook" }).click();
  await expect(page.locator("#view-title")).toHaveText("Logbook");
  await expect(page.locator("#new-heading")).toBeHidden();
  const month = (value) => page.evaluate((date) => new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" }).format(new Date(date)), value);
  // Grouped by month, newest first, like the Things Logbook.
  await expect(page.locator(".task-section h2")).toHaveText([await month(now.toISOString()), await month("2025-01-02T10:00:00Z")]);
  await expect(page.locator("#quick-add")).toBeHidden();
  await page.getByRole("button", { name: "Mark open Filed taxes" }).click();
  await expect(page.locator(".task-title", { hasText: "Filed taxes" })).toBeHidden();
  await expect.poll(() => calls[0]).toEqual(["reopen", { uuid: "l1" }]);

  await page.locator(".nav-item", { hasText: "Trash" }).click();
  await page.getByRole("button", { name: "Restore" }).click();
  await expect(page.locator(".toast.success", { hasText: "Restored “Bad idea”" })).toBeVisible();
  expect(calls[1]).toEqual(["restore", { uuids: ["t9"] }]);
});

test("Empty Trash asks first and deletes everything in the Trash", async ({ page }) => {
  let trash = [
    { uuid: "t1", title: "Bad idea", type: "task", status: "open" },
    { uuid: "p1", title: "Old plan", type: "project", status: "open" },
  ];
  let emptied = 0;
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([]) }),
    "GET /api/trash": (route) => route.fulfill({ json: { total: trash.length, items: trash } }),
    "POST /api/empty-trash": (route) => {
      emptied++;
      trash = [];
      return route.fulfill({ json: { ok: true, deleted: 2 } });
    },
  });
  const dialogs = [];
  page.on("dialog", (dialog) => {
    dialogs.push(dialog.message());
    return dialogs.length === 1 ? dialog.dismiss() : dialog.accept();
  });
  await page.goto("/");
  await page.locator(".nav-item", { hasText: "Trash" }).click();
  await expect(page.locator(".task-title")).toHaveText(["Bad idea", "Old plan"]);

  // Cancelling the question keeps everything.
  await page.getByRole("button", { name: "Empty Trash" }).click();
  await expect.poll(() => dialogs[0]).toBe("Empty the Trash? 2 items will be deleted for good. This cannot be undone.");
  await expect(page.locator(".task-title")).toHaveCount(2);
  expect(emptied).toBe(0);

  await page.getByRole("button", { name: "Empty Trash" }).click();
  await expect(page.locator(".empty")).toHaveText("The Trash is empty.");
  await expect(page.locator(".toast.success", { hasText: "Emptied the Trash" })).toBeVisible();
  expect(emptied).toBe(1);
  await expect(page.getByRole("button", { name: "Empty Trash" })).toHaveCount(0);
});

test("headings: create one and drag to-dos under it", async ({ page }) => {
  const calls = [];
  const inProject = (id, title, heading = "") => ({ ...task(id, title, ["anytime"]), project: "p1", projectTitle: "Relocation", heading, headingTitle: heading ? "Küche" : "" });
  const data = model([inProject("a", "Kisten"), inProject("b", "Herd", "h1"), inProject("c", "Lampen")], {
    projects: [{ uuid: "p1", title: "Relocation", index: 1, area: "", when: "anytime" }],
    areas: [],
    headings: [
      { uuid: "h1", title: "Küche", project: "p1", index: 10 },
      { uuid: "h2", title: "Bad", project: "p1", index: 20 },
    ],
  });
  // Like the server, later syncs show the changes.
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    calls.push({ path, body });
    if (path === "/api/move" && body.heading) {
      for (const id of body.uuids) {
        const moved = data.tasks.find((t) => t.uuid === id);
        Object.assign(moved, { heading: body.heading, headingTitle: data.headings.find((h) => h.uuid === body.heading).title });
      }
    }
    if (path === "/api/create") data.headings.push({ uuid: "h3", title: body.title, project: body.project, index: 30 });
    return route.fulfill({ json: { ok: true, uuid: "h3", reordered: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/move": record,
    "POST /api/reorder": record,
    "POST /api/create": record,
  });
  await page.goto("/");
  await page.locator("#projects .mini-item", { hasText: "Relocation" }).click();
  await expect(page.locator(".task-section h2.heading")).toHaveText(["Küche", "Bad"]);

  // Onto the empty heading "Bad".
  const from = await page.locator(".task-row", { hasText: "Lampen" }).boundingBox();
  const to = await page.locator("h2.heading", { hasText: "Bad" }).boundingBox();
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect.poll(() => calls[0]).toEqual({ path: "/api/move", body: { uuids: ["c"], heading: "h2" } });
  await expect(page.locator('[data-section-heading="h2"] .task-title')).toHaveText(["Lampen"]);

  // New heading at the end.
  await page.getByRole("button", { name: "New Heading" }).click();
  await page.locator(".heading-new").fill("Keller");
  await page.locator(".heading-new").press("Enter");
  await expect(page.locator(".task-section h2.heading")).toHaveText(["Küche", "Bad", "Keller"]);
  await expect.poll(() => calls.filter((call) => call.path === "/api/create")).toEqual([
    { path: "/api/create", body: { type: "heading", title: "Keller", project: "p1" } },
  ]);
});

test("headings are renamed by double click and deleted from the right-click menu; their to-dos stay", async ({ page }) => {
  const calls = [];
  const inProject = (id, title, heading = "") => ({ ...task(id, title, ["anytime"]), project: "p1", projectTitle: "Relocation", heading, headingTitle: heading ? "Küche" : "" });
  const data = model([inProject("a", "Kisten"), inProject("b", "Herd", "h1")], {
    projects: [{ uuid: "p1", title: "Relocation", index: 1, area: "", when: "anytime" }],
    areas: [],
    headings: [{ uuid: "h1", title: "Küche", project: "p1", index: 10 }],
  });
  // Like the server, later syncs show the changes.
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    calls.push({ path, body });
    if (path === "/api/update") data.headings[0].title = body.title;
    if (path === "/api/heading-delete") {
      data.headings = [];
      for (const item of data.tasks) Object.assign(item, { heading: "", headingTitle: "" });
    }
    return route.fulfill({ json: { ok: true, updated: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/update": record,
    "POST /api/heading-delete": record,
  });
  await page.goto("/");
  await page.locator("#projects .mini-item", { hasText: "Relocation" }).click();
  await page.locator("h2.heading", { hasText: "Küche" }).dblclick();
  await page.locator("h2.heading .rename-input").fill("Bad");
  await page.locator("h2.heading .rename-input").press("Enter");
  await expect.poll(() => calls[0]).toEqual({ path: "/api/update", body: { uuid: "h1", title: "Bad" } });
  await expect(page.locator("h2.heading")).toHaveText(["Bad"]);

  page.once("dialog", (dialog) => dialog.accept());
  await page.locator("h2.heading", { hasText: "Bad" }).click({ button: "right" });
  await page.locator(".context-menu").getByRole("menuitem", { name: "Delete Heading…" }).click();
  await expect.poll(() => calls[1]).toEqual({ path: "/api/heading-delete", body: { uuid: "h1" } });
  await expect(page.locator("h2.heading")).toHaveCount(0);
  await expect(page.locator(".task-title")).toHaveText(["Kisten", "Herd"]);
});

test("an area is deleted from the sidebar; its contents go to the Trash and come back without it", async ({ page }) => {
  const calls = [];
  const inArea = (id, title, project = "") => ({ ...task(id, title, ["anytime"]), area: "a-home", areaTitle: "Home", project, projectTitle: project ? "Relocation" : "" });
  const items = [inArea("t-water", "Water"), inArea("t-box", "Boxes", "p-move")];
  const data = model([...items, task("t-other", "Other", ["anytime"])], {
    areas: [{ uuid: "a-home", title: "Home", index: 1, count: 2 }],
    projects: [{ uuid: "p-move", title: "Relocation", index: 1, area: "a-home", when: "anytime", count: 1 }],
  });
  const projects = data.projects;
  // Like the server, later syncs show the changes.
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    calls.push({ path, body });
    if (path === "/api/area-delete") {
      data.areas = [];
      data.projects = [];
      data.tasks = data.tasks.filter((item) => item.area !== "a-home");
      return route.fulfill({ json: { ok: true, trashed: ["p-move", "t-water"], count: 3 } });
    }
    data.projects = projects.map((project) => ({ ...project, area: "" }));
    data.tasks.push(...items.map((item) => ({ ...item, area: "", areaTitle: "" })));
    return route.fulfill({ json: { ok: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/area-delete": record,
    "POST /api/restore": record,
  });
  const dialogs = [];
  page.on("dialog", (dialog) => {
    dialogs.push(dialog.message());
    return dialogs.length === 1 ? dialog.dismiss() : dialog.accept();
  });
  await page.goto("/");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Home", "Relocation"]);

  // Right click offers Delete Area with the counts; cancelling keeps it.
  await page.locator("#projects .mini-item", { hasText: "Home" }).click({ button: "right" });
  await expect(page.locator(".context-menu").getByRole("menuitem", { name: "Delete Project…" })).toHaveCount(0);
  await page.locator(".context-menu").getByRole("menuitem", { name: "Delete Area…" }).click();
  await expect.poll(() => dialogs[0]).toBe("Delete the area “Home”? Its 1 project and 2 to-dos move to the Trash.");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Home", "Relocation"]);
  expect(calls).toEqual([]);

  await page.locator("#projects .mini-item", { hasText: "Home" }).click({ button: "right" });
  await page.locator(".context-menu").getByRole("menuitem", { name: "Delete Area…" }).click();
  await expect.poll(() => calls[0]).toEqual({ path: "/api/area-delete", body: { uuid: "a-home" } });
  await expect(page.locator("#projects .mini-item[data-scope-id]")).toHaveCount(0);

  // Undo restores the projects and to-dos; the area stays deleted.
  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => calls[1]).toEqual({ path: "/api/restore", body: { uuids: ["p-move", "t-water"] } });
  await expect(page.locator("#projects .mini-title")).toHaveText(["Relocation"]);
  await expect(page.locator(".toast").last()).toContainText("the area stays deleted");
});

test("projects move between areas and areas are reordered and created", async ({ page }) => {
  const calls = [];
  const data = model([], {
    areas: [
      { uuid: "a-private", title: "Private", index: 1 },
      { uuid: "a-study", title: "Study", index: 2 },
    ],
    projects: [
      { uuid: "p-work", title: "Work", index: 0, area: "", when: "anytime" },
      { uuid: "p-bike", title: "Bike", index: 5, area: "a-private", when: "anytime" },
      { uuid: "p-thesis", title: "Thesis", index: 1, area: "a-study", when: "anytime" },
    ],
  });
  // Like the server, later syncs show the changes.
  const record = (route, request) => {
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    calls.push({ path, body });
    if (path === "/api/project-area") data.projects.find((p) => p.uuid === body.uuid).area = body.area;
    if (path === "/api/reorder" && body.view === "areas") body.order.forEach((id, i) => (data.areas.find((a) => a.uuid === id).index = i));
    if (path === "/api/area-create") data.areas.push({ uuid: "a-new", title: body.title, index: 9 });
    return route.fulfill({ json: { ok: true, uuid: "a-new", title: "Garten", reordered: true } });
  };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: data }),
    "POST /api/project-area": record,
    "POST /api/reorder": record,
    "POST /api/area-create": record,
  });
  // The sidebar is redrawn after each sync: measure once it has settled.
  const box = async (text) => {
    const item = page.locator("#projects .mini-item", { hasText: text });
    let found = null;
    await expect.poll(async () => (found = await item.boundingBox())).not.toBeNull();
    return found;
  };
  const drag = async (fromText, toText, fraction = 0.5) => {
    await page.waitForLoadState("networkidle");
    const from = await box(fromText);
    const to = await box(toText);
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(to.x + to.width / 2, to.y + to.height * fraction, { steps: 8 });
    await page.mouse.up();
  };
  await page.goto("/");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Work", "Private", "Bike", "Study", "Thesis"]);

  await drag("Bike", "Study");
  await expect(page.locator("#projects .mini-title")).toHaveText(["Work", "Private", "Study", "Thesis", "Bike"]);
  await expect.poll(() => calls[0]).toEqual({ path: "/api/project-area", body: { uuid: "p-bike", area: "a-study" } });

  await drag("Study", "Private", 0.2);
  await expect(page.locator("#projects .mini-title")).toHaveText(["Work", "Study", "Thesis", "Bike", "Private"]);
  await expect.poll(() => calls.at(-1)).toEqual({ path: "/api/reorder", body: { view: "areas", moved: "a-study", order: ["a-study", "a-private"] } });

  await page.getByRole("button", { name: "New Area" }).click();
  await page.locator(".area-new").fill("Garten");
  await page.locator(".area-new").press("Enter");
  await expect(page.locator("#projects .mini-title").last()).toHaveText("Garten");
  await expect.poll(() => calls.at(-1)).toEqual({ path: "/api/area-create", body: { title: "Garten" } });
});

test("checklist items are reordered by dragging their handle", async ({ page }) => {
  let request = null;
  const withItems = { ...task("t1", "Pack"), checklist: [
    { uuid: "i1", title: "Passport", done: false },
    { uuid: "i2", title: "Charger", done: false },
    { uuid: "i3", title: "Shoes", done: false },
  ] };
  await mockApp(page, {
    "GET /api/status": signedIn,
    "GET /api/sync": (route) => route.fulfill({ json: model([withItems]) }),
    "POST /api/checklist": (route, req) => {
      request = req.postDataJSON();
      return route.fulfill({ json: { ok: true } });
    },
  });
  await page.goto("/");
  await page.locator(".task-row", { hasText: "Pack" }).dblclick();
  const items = page.locator(".task-row.editing .check-item[data-item]:not(.new)");
  const values = () => items.locator(".item-title").evaluateAll((inputs) => inputs.map((input) => input.value));
  await expect.poll(values).toEqual(["Passport", "Charger", "Shoes"]);
  await items.nth(2).hover();
  const handle = await items.nth(2).locator(".item-handle").boundingBox();
  const first = await items.nth(0).boundingBox();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.mouse.move(first.x + first.width / 2, first.y + first.height * 0.2, { steps: 8 });
  await page.mouse.up();
  await expect.poll(values).toEqual(["Shoes", "Passport", "Charger"]);
  expect(request).toEqual({ action: "reorder", task: "t1", uuid: "i3", order: ["i3", "i1", "i2"] });
});

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, viewport: { width: 390, height: 844 } });

  test("a long press selects to-dos and taps add to the selection", async ({ page }) => {
    await mockApp(page, {
      "GET /api/status": signedIn,
      "GET /api/sync": (route) => route.fulfill({ json: model([task("a", "Alpha"), task("b", "Beta")]) }),
    });
    await page.goto("/");
    const press = async (text, ms) => {
      const row = page.locator(".task-row", { hasText: text });
      const box = await row.boundingBox();
      const point = { x: box.x + 60, y: box.y + box.height / 2 };
      await page.evaluate(
        ({ point, ms }) =>
          new Promise((resolve) => {
            const target = document.elementFromPoint(point.x, point.y);
            const init = { bubbles: true, cancelable: true, pointerType: "touch", clientX: point.x, clientY: point.y, isPrimary: true };
            target.dispatchEvent(new PointerEvent("pointerdown", init));
            setTimeout(() => {
              target.dispatchEvent(new PointerEvent("pointerup", init));
              target.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, clientX: point.x, clientY: point.y }));
              resolve();
            }, ms);
          }),
        { point, ms },
      );
    };
    await press("Alpha", 600);
    await expect(page.locator(".task-row.selected")).toHaveCount(1);
    await expect(page.locator(".task-row.editing")).toHaveCount(0);
    await press("Beta", 50);
    await expect(page.locator(".task-row.selected")).toHaveCount(2);
  });
});

test.describe("shortcuts after Things", () => {
  const tomorrowISO = () => {
    const date = new Date();
    date.setDate(date.getDate() + 1);
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
  };

  async function setup(page, calls, tasks) {
    const data = model(tasks, {
      areas: [],
      projects: [{ uuid: "p-move", title: "Relocation", index: 1, area: "", when: "anytime" }],
      headings: [],
    });
    const record = (route, request) => {
      calls.push({ path: new URL(request.url()).pathname, body: request.postDataJSON() });
      return route.fulfill({ json: { ok: true, uuid: `new-${calls.length}`, task: { ...task(`new-${calls.length}`, "x", ["anytime"]) } } });
    };
    await mockApp(page, {
      "GET /api/status": signedIn,
      "GET /api/sync": (route) => route.fulfill({ json: data }),
      "POST /api/schedule": record,
      "POST /api/deadline": record,
      "POST /api/move": record,
      "POST /api/create": record,
      "POST /api/reorder": record,
      "POST /api/tags": record,
      "POST /api/checklist": record,
    });
    await page.goto("/");
    await page.keyboard.press("4");
    await expect(page.locator("#view-title")).toHaveText("Anytime");
  }

  test("Alt+T starts the selection Today; Ctrl+R and Ctrl+O set Anytime and Someday", async ({ page }) => {
    const calls = [];
    await setup(page, calls, [task("a", "Alpha", ["anytime"]), task("b", "Beta", ["anytime"]), task("c", "Gamma", ["anytime"])]);
    await page.locator(".task-row", { hasText: "Alpha" }).click();
    await page.locator(".task-row", { hasText: "Beta" }).click({ modifiers: ["Shift"] });
    await page.keyboard.press("Alt+t");
    await expect.poll(() => calls[0]).toEqual({ path: "/api/schedule", body: { uuids: ["a", "b"], when: "today" } });
    await expect(page.locator(".toast.success")).toContainText("2 to-dos → Today");
    await page.locator(".task-row", { hasText: "Gamma" }).click();
    await page.keyboard.press("Control+o");
    await expect.poll(() => calls[1]).toEqual({ path: "/api/schedule", body: { uuids: ["c"], when: "someday" } });
    // Ctrl+R is Anytime, not a page reload.
    await page.keyboard.press("Control+r");
    await expect.poll(() => calls[2]).toEqual({ path: "/api/schedule", body: { uuids: ["c"], when: "anytime" } });
    await expect(page.locator(".task-row.selected")).toHaveCount(1);
  });

  test("Ctrl+] and Ctrl+. move start date and deadline", async ({ page }) => {
    const calls = [];
    await setup(page, calls, [task("a", "Alpha", ["anytime"])]);
    await page.locator(".task-row", { hasText: "Alpha" }).click();
    await page.keyboard.press("Control+BracketRight");
    await expect.poll(() => calls[0]).toEqual({ path: "/api/schedule", body: { uuids: ["a"], startDate: tomorrowISO() } });
    await page.keyboard.press("Control+Period");
    await expect.poll(() => calls[1]).toEqual({ path: "/api/deadline", body: { uuid: "a", deadline: tomorrowISO() } });
  });

  test("Ctrl+Shift+M moves the selection with the Move dialog; Ctrl+Shift+O navigates", async ({ page }) => {
    const calls = [];
    await setup(page, calls, [task("a", "Alpha", ["anytime"])]);
    await page.locator(".task-row", { hasText: "Alpha" }).click();
    await page.keyboard.press("Control+Shift+m");
    await page.keyboard.type("rel");
    await page.keyboard.press("Enter");
    await expect.poll(() => calls[0]).toEqual({ path: "/api/move", body: { uuids: ["a"], project: "p-move" } });
    await page.keyboard.press("Control+Shift+o");
    await page.keyboard.type("relocation");
    await page.keyboard.press("Enter");
    await expect(page.locator("#view-title")).toHaveText("Relocation");
    await page.keyboard.press("Control+ArrowLeft");
    await expect(page.locator("#view-title")).toHaveText("Anytime");
  });

  test("Ctrl+A selects all, Space adds below the selection, paste adds lines", async ({ page }) => {
    const calls = [];
    await setup(page, calls, [task("a", "Alpha", ["anytime"]), task("b", "Beta", ["anytime"])]);
    await page.locator(".task-row", { hasText: "Alpha" }).click();
    await page.keyboard.press("Control+a");
    await expect(page.locator(".task-row.selected")).toHaveCount(2);
    await page.locator(".task-row", { hasText: "Alpha" }).click();
    await page.keyboard.press("Space");
    await expect(page.locator("#quick-add input[name=title]")).toBeFocused();
    await page.keyboard.type("Between");
    await page.keyboard.press("Enter");
    await expect.poll(() => calls.map((call) => call.path)).toEqual(["/api/create", "/api/reorder"]);
    expect(calls[1].body.order.indexOf("a") + 1).toBe(calls[1].body.order.indexOf(calls[1].body.moved));

    await page.locator(".task-list").click({ position: { x: 5, y: 5 } });
    await page.evaluate(() => {
      const data = new DataTransfer();
      data.setData("text/plain", "- Milk\n- Bread\n");
      document.body.dispatchEvent(new ClipboardEvent("paste", { clipboardData: data, bubbles: true }));
    });
    await expect.poll(() => calls.filter((call) => call.path === "/api/create").map((call) => call.body.title).sort()).toEqual(["Between", "Bread", "Milk"]);
  });

  test("the cheat sheet lists the shortcuts", async ({ page }) => {
    const calls = [];
    await setup(page, calls, [task("a", "Alpha", ["anytime"])]);
    await page.keyboard.press("?");
    const sheet = page.locator("#shortcuts-dialog");
    await expect(sheet).toBeVisible();
    const today = sheet.locator("tr", { hasText: "Start Today" });
    await expect(today).toContainText("Alt");
    await expect(today).toContainText("kept by the browser");
    // Every listed shortcut works in the web app now.
    await expect(sheet.locator("tr.unavailable")).toHaveCount(0);
    await expect(sheet.locator("tr", { hasText: "Bold, italic in notes" })).toBeVisible();
    await expect(sheet.locator("tr", { hasText: "Add repetition schedule" })).not.toHaveClass(/unavailable/);
    await expect(sheet.locator("tr", { hasText: "Start This Evening" })).not.toHaveClass(/unavailable/);
    await page.keyboard.press("Escape");
    await expect(sheet).toBeHidden();
    await page.getByRole("button", { name: "Shortcuts" }).click();
    await expect(sheet).toBeVisible();
  });
});
