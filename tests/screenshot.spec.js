// Writes docs/screenshot.png from demo data (no Things account):
// npm run screenshot. Skipped in the normal test run.
const { test } = require("@playwright/test");

test.skip(!process.env.SCREENSHOT, "set SCREENSHOT=1 to write docs/screenshot.png");

const today = new Date().toISOString().slice(0, 10);
const inDays = (days) => new Date(Date.now() + days * 86400000).toISOString().slice(0, 10);

function todo(uuid, title, extra = {}) {
  const views = extra.views || ["today"];
  return {
    uuid,
    title,
    note: "",
    status: "open",
    type: "task",
    schedule: views[0],
    scheduledDate: today,
    deadline: "",
    project: "",
    projectTitle: "",
    heading: "",
    headingTitle: "",
    area: "",
    areaTitle: "",
    tags: [],
    tagIDs: [],
    checklist: [],
    evening: false,
    repeat: "",
    reminder: "",
    views,
    ...extra,
  };
}

const tags = [
  { uuid: "tag-errand", title: "Errand", index: 0 },
  { uuid: "tag-calls", title: "Calls", index: 1 },
];
const areas = [
  { uuid: "a-home", title: "Home", type: "area", index: 0, count: 3 },
  { uuid: "a-work", title: "Work", type: "area", index: 1, count: 2 },
];
const projects = [
  { uuid: "p-garden", title: "Spring Garden", type: "project", area: "a-home", when: "anytime", index: 0, count: 2, status: "open", done: 3, total: 5, tags: [], tagIDs: [] },
  { uuid: "p-launch", title: "Website Launch", type: "project", area: "a-work", when: "anytime", index: 1, count: 2, status: "open", done: 1, total: 4, tags: [], tagIDs: [] },
  { uuid: "p-trip", title: "Weekend Trip", type: "project", area: "", when: "anytime", index: -1, count: 1, status: "open", done: 0, total: 2, tags: [], tagIDs: [] },
];
const tasks = [
  todo("t1", "Buy seeds and compost", { project: "p-garden", projectTitle: "Spring Garden", area: "a-home", areaTitle: "Home", tags: ["Errand"], tagIDs: ["tag-errand"], checklist: [
    { uuid: "c1", title: "Tomatoes", done: true, index: 0 },
    { uuid: "c2", title: "Basil", done: false, index: 1 },
  ] }),
  todo("t2", "Call the plumber", { area: "a-home", areaTitle: "Home", tags: ["Calls"], tagIDs: ["tag-calls"], reminder: "10:30" }),
  todo("t3", "Review the landing page copy", { project: "p-launch", projectTitle: "Website Launch", area: "a-work", areaTitle: "Work", deadline: inDays(2), note: "Check the **pricing** section." }),
  todo("t4", "Send the release notes", { project: "p-launch", projectTitle: "Website Launch", area: "a-work", areaTitle: "Work", status: "completed" }),
  todo("t5", "Book a table for Saturday", { project: "p-trip", projectTitle: "Weekend Trip" }),
  todo("t6", "Stretch for ten minutes", { repeat: "Every day" }),
  todo("t7", "Read a chapter", { evening: true }),
  todo("t8", "Water the plants", { area: "a-home", areaTitle: "Home", evening: true }),
];
const lists = { today: tasks, inbox: [], anytime: tasks, upcoming: [], someday: [] };

test("screenshot", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.route("**/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/status") return route.fulfill({ json: { signedIn: true, serverManaged: true, backend: { cli: "things-cli" } } });
    if (path === "/api/sync") {
      const now = new Date().toISOString();
      return route.fulfill({ json: { generatedAt: now, syncedAt: now, tasks, projects, areas, tags, headings: [], lists } });
    }
    return route.abort();
  });
  await page.goto("/");
  await page.locator(".task-row", { hasText: "Buy seeds" }).waitFor();
  await page.screenshot({ path: "docs/screenshot.png" });
});
