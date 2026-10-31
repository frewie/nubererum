const test = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const { readFileSync } = require("node:fs");
const path = require("node:path");

const { appRequestHeaders, compareManifests } = require("../scripts/compare-things-parity");

const fixtures = path.join(__dirname, "fixtures", "parity");
const referencePath = path.join(fixtures, "reference.json");
const matchingAppPath = path.join(fixtures, "matching-app.json");
const mismatchedAppPath = path.join(fixtures, "mismatched-app.json");
const reference = JSON.parse(readFileSync(referencePath, "utf8"));
const matchingApp = JSON.parse(readFileSync(matchingAppPath, "utf8"));
const mismatchedApp = JSON.parse(readFileSync(mismatchedAppPath, "utf8"));

function appModel(lists) {
  const tasks = Object.values(lists).flat();
  return {
    generatedAt: "2026-08-17T12:00:00Z",
    tasks,
    projects: [],
    areas: [],
    lists,
  };
}

function task(uuid, properties = {}) {
  return { uuid, status: "open", views: [], ...properties };
}

test("matching list membership and order passes parity", () => {
  const result = compareManifests(reference, matchingApp);

  assert.equal(result.ok, true);
  assert.deepEqual(result.findings, []);
});

test("reports completed status and hierarchy mismatches", () => {
  const result = compareManifests(
    reference,
    appModel({
      today: [task("today-first", { status: "completed", project: "wrong-project", area: "wrong-area" }), task("today-second")],
      inbox: [task("inbox-task")],
      anytime: [task("anytime-first", { area: "wrong-area" }), task("anytime-second")],
      upcoming: [],
      someday: [],
    }),
  );

  assert.ok(result.findings.some((finding) => finding.type === "status" && finding.list === "today"));
  assert.ok(result.findings.some((finding) => finding.type === "project" && finding.list === "today"));
  assert.ok(result.findings.some((finding) => finding.type === "area" && finding.list === "anytime"));
});

test("accepts completed to-dos the app shows where Things shows them, and reports differences", () => {
  const completedReference = structuredClone(reference);
  completedReference.lists.inbox[0].status = "completed";
  const completedApp = structuredClone(matchingApp);
  completedApp.lists.inbox[0].status = "completed";

  const same = compareManifests(completedReference, completedApp);
  assert.ok(!same.findings.some((finding) => finding.list === "inbox"), JSON.stringify(same.findings));

  const logged = structuredClone(completedApp);
  logged.lists.inbox.splice(0, 1);
  assert.ok(compareManifests(completedReference, logged).findings.some((finding) => finding.type === "missing-completed"));

  const extra = structuredClone(matchingApp);
  extra.lists.inbox.push({ uuid: "done-elsewhere", status: "completed" });
  assert.ok(compareManifests(reference, extra).findings.some((finding) => finding.type === "unexpected-completed"));
});

test("reports unexpected project and direct-area relationships", () => {
  const relatedApp = structuredClone(matchingApp);
  relatedApp.lists.inbox[0].project = "unexpected-project";
  relatedApp.lists.anytime[0].area = "unexpected-area";

  const result = compareManifests(reference, relatedApp);

  assert.ok(result.findings.some((finding) => finding.type === "project" && finding.list === "inbox"));
  assert.ok(result.findings.some((finding) => finding.type === "area" && finding.list === "anytime"));
});

test("rejects duplicate Logbook UUIDs", () => {
  const duplicateReference = structuredClone(reference);
  duplicateReference.lists.logbook.push(duplicateReference.lists.logbook[0]);

  const result = compareManifests(
    duplicateReference,
    appModel({
      today: [task("today-first", { project: "project-1", area: "area-1" }), task("today-second")],
      inbox: [task("inbox-task")],
      anytime: [task("anytime-first"), task("anytime-second")],
      upcoming: [],
      someday: [],
    }),
  );

  assert.ok(result.findings.some((finding) => finding.type === "duplicate-reference" && finding.list === "logbook"));
});

test("accepts a reference without Logbook and still reports leaked tasks", () => {
  const activeReference = structuredClone(reference);
  delete activeReference.lists.logbook;

  assert.equal(compareManifests(activeReference, matchingApp).ok, true);

  const result = compareManifests(activeReference, mismatchedApp);
  assert.ok(result.findings.some((finding) => finding.type === "unexpected" && finding.list === "inbox"));
  assert.ok(!result.findings.some((finding) => finding.type === "completed-leak"));
});

test("ignores Things projects and matches to-dos completed today", () => {
  const withExtras = structuredClone(reference);
  withExtras.lists.today.push({ uuid: "done-today", type: "to do", status: "completed", position: 2, project: "", area: "" });
  withExtras.lists.anytime.unshift({ uuid: "project-1", type: "project", status: "open", position: 0, project: "", area: "" });
  const appWithDone = structuredClone(matchingApp);
  appWithDone.lists.today.push({ uuid: "done-today", status: "completed", project: "", area: "" });

  assert.deepEqual(compareManifests(withExtras, appWithDone).findings, []);
});

test("Upcoming is ordered by day only: AppleScript does not expose the order inside a day", () => {
  const lists = { today: [], inbox: [], anytime: [], someday: [] };
  const manifest = (upcoming) => ({ schemaVersion: 1, lists: { ...lists, upcoming } });
  const app = appModel({
    ...lists,
    upcoming: [
      task("a", { scheduledDate: "2026-10-31" }),
      task("b", { scheduledDate: "2026-10-31" }),
      // A repeating template: the app lists it under todayDate.
      task("c", { scheduledDate: "", todayDate: "2026-11-02" }),
    ],
  });
  assert.deepEqual(compareManifests(manifest(["b", "a", "c"]), app).findings, []);
  const result = compareManifests(manifest(["c", "a", "b"]), app);
  assert.ok(result.findings.some((finding) => finding.type === "order" && finding.list === "upcoming" && finding.index === 0));
});

test("reports empty Today, wrong order, and completed-task leakage", () => {
  const result = compareManifests(reference, mismatchedApp);

  assert.equal(result.ok, false);
  assert.ok(result.findings.some((finding) => finding.type === "missing" && finding.list === "today"));
  assert.ok(result.findings.some((finding) => finding.type === "order" && finding.list === "anytime"));
  assert.ok(result.findings.some((finding) => finding.type === "completed-leak" && finding.list === "inbox"));
});

test("rejects duplicate UUIDs because order would be ambiguous", () => {
  const result = compareManifests(
    reference,
    appModel({
      today: [task("today-first"), task("today-first"), task("today-second")],
      inbox: [task("inbox-task")],
      anytime: [task("anytime-first"), task("anytime-second")],
      upcoming: [],
      someday: [],
    }),
  );

  assert.ok(result.findings.some((finding) => finding.type === "duplicate" && finding.list === "today"));
});

test("builds Basic Auth without placing credentials in the URL", () => {
  const headers = appRequestHeaders({ PARITY_APP_USERNAME: "tester", PARITY_APP_PASSWORD: "secret" });

  assert.equal(headers.authorization, `Basic ${Buffer.from("tester:secret").toString("base64")}`);
});

test("reports a descriptive error for malformed app responses", () => {
  assert.throws(() => compareManifests(reference, {}), /App response is missing lists\.today/);
});

test("CLI exits nonzero and reports privacy-safe mismatch details", () => {
  const result = spawnSync(
    process.execPath,
    [path.join(__dirname, "..", "scripts", "compare-things-parity.js"), "--reference", referencePath, "--actual", mismatchedAppPath],
    { encoding: "utf8" },
  );

  assert.equal(result.status, 1);
  assert.match(result.stdout, /FAIL:/);
  assert.match(result.stdout, /today\s+Things\s+2\s+App\s+0/);
  assert.doesNotMatch(result.stdout, /task title|notes/i);
});
