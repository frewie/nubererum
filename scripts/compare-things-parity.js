#!/usr/bin/env node

const fs = require("node:fs/promises");

const OPEN_LISTS = ["today", "inbox", "anytime", "upcoming", "someday"];

function itemUUID(item) {
  if (typeof item === "string") return item;
  return String(item?.uuid || item?.id || "").trim();
}

function duplicateUUIDs(ids) {
  const seen = new Set();
  const duplicates = new Set();
  for (const id of ids) {
    if (seen.has(id)) duplicates.add(id);
    seen.add(id);
  }
  return [...duplicates];
}

function isOpen(item) {
  return typeof item === "string" || String(item?.status || "open").toLowerCase() === "open";
}

// Lists are compared on their open to-dos. Things also shows projects there,
// which the app lists separately, and completed to-dos until they are logged,
// which are compared on their own.
function isExpectedInApp(item) {
  if (typeof item === "string") return true;
  const status = String(item?.status || "open").toLowerCase();
  return status === "open" && String(item?.type || "").toLowerCase() !== "project";
}

function referenceItems(manifest, list) {
  const items = manifest?.lists?.[list];
  if (!Array.isArray(items)) throw new Error(`Reference manifest is missing lists.${list}.`);
  return list === "logbook" ? items : items.filter(isExpectedInApp);
}

function manifestList(manifest, list) {
  const ids = referenceItems(manifest, list).map(itemUUID);
  if (ids.some((id) => !id)) throw new Error(`Reference manifest contains an empty UUID in lists.${list}.`);
  return ids;
}

function appList(model, list) {
  const items = model?.lists?.[list];
  if (!Array.isArray(items)) throw new Error(`App response is missing lists.${list}.`);
  const ids = items.filter(isOpen).map(itemUUID);
  if (ids.some((id) => !id)) throw new Error(`App response contains an empty UUID in lists.${list}.`);
  return ids;
}

// upcomingDay is the day the app lists an Upcoming to-do under (repeating
// templates carry it as todayDate).
function upcomingDay(item) {
  return String(item?.scheduledDate || item?.todayDate || "");
}

function completedIDs(items) {
  return new Set(
    items
      .filter((item) => !isOpen(item) && String(item?.type || "").toLowerCase() !== "project")
      .map(itemUUID),
  );
}

function itemByUUID(items) {
  return new Map(items.map((item) => [itemUUID(item), item]));
}

function compareManifests(reference, model) {
  if (reference?.schemaVersion !== 1) {
    throw new Error(`Unsupported reference schema version: ${reference?.schemaVersion ?? "missing"}.`);
  }

  const findings = [];
  // Logbook is optional: completed tasks in an open app list are already reported as unexpected.
  const logbook = reference.lists?.logbook ? manifestList(reference, "logbook") : [];
  const logbookDuplicates = duplicateUUIDs(logbook);
  if (logbookDuplicates.length) {
    findings.push({ type: "duplicate-reference", list: "logbook", uuids: logbookDuplicates });
  }
  const completed = new Set(logbook);
  const counts = {};

  for (const list of OPEN_LISTS) {
    const expected = manifestList(reference, list);
    const actual = appList(model, list);
    const expectedItems = referenceItems(reference, list);
    const actualItems = model.lists[list];
    const expectedSet = new Set(expected);
    const actualSet = new Set(actual);
    const expectedByUUID = itemByUUID(expectedItems);
    const actualByUUID = itemByUUID(actualItems);
    counts[list] = { expected: expected.length, actual: actual.length };

    const expectedDuplicates = duplicateUUIDs(expected);
    if (expectedDuplicates.length) {
      findings.push({ type: "duplicate-reference", list, uuids: expectedDuplicates });
    }

    const actualDuplicates = duplicateUUIDs(actual);
    if (actualDuplicates.length) {
      findings.push({ type: "duplicate", list, uuids: actualDuplicates });
    }

    const missing = expected.filter((id) => !actualSet.has(id));
    if (missing.length) findings.push({ type: "missing", list, uuids: missing });

    const unexpected = actual.filter((id) => !expectedSet.has(id));
    if (unexpected.length) findings.push({ type: "unexpected", list, uuids: unexpected });

    // An open Things to-do shown as completed in the app.
    const wrongStatus = expected.filter((id) => {
      const item = actualItems.find((candidate) => itemUUID(candidate) === id);
      return item && !isOpen(item);
    });
    if (wrongStatus.length) findings.push({ type: "status", list, uuids: wrongStatus });

    // Completed to-dos the app still shows must be the ones Things shows.
    const appDone = completedIDs(actualItems);
    const thingsDone = completedIDs(reference.lists[list]);
    const extraDone = [...appDone].filter((id) => !thingsDone.has(id) && !expectedSet.has(id));
    if (extraDone.length) findings.push({ type: "unexpected-completed", list, uuids: extraDone });
    const missingDone = [...thingsDone].filter((id) => !appDone.has(id));
    if (missingDone.length) findings.push({ type: "missing-completed", list, uuids: missingDone });

    const completedLeaks = actual.filter((id) => completed.has(id));
    if (completedLeaks.length) {
      findings.push({ type: "completed-leak", list, uuids: [...new Set(completedLeaks)] });
    }

    for (const id of expected) {
      const expectedItem = expectedByUUID.get(id);
      const actualItem = actualByUUID.get(id);
      if (!actualItem || typeof expectedItem === "string" || typeof actualItem === "string") continue;

      const expectedStatus = String(expectedItem.status || "").toLowerCase();
      const actualStatus = String(actualItem.status || "").toLowerCase();
      if (expectedStatus && !actualStatus) {
        findings.push({ type: "status", list, uuids: [id] });
      }

      const expectedProject = String(expectedItem.project || "");
      const actualProject = String(actualItem.project || "");
      if (actualProject !== expectedProject) {
        findings.push({ type: "project", list, uuids: [id] });
      }

      // Things exposes a task's direct area; the app also inherits area from its project.
      if (!expectedProject) {
        const expectedArea = String(expectedItem.area || "");
        const actualArea = String(actualItem.area || "");
        if (actualArea !== expectedArea) {
          findings.push({ type: "area", list, uuids: [id] });
        }
      }
    }

    const sameMembers =
      expected.length === actual.length &&
      expectedDuplicates.length === 0 &&
      actualDuplicates.length === 0 &&
      missing.length === 0 &&
      unexpected.length === 0;
    if (sameMembers) {
      // Upcoming is compared by day: AppleScript returns the days in order
      // but not the order Things shows inside one day, so the Things
      // sequence must only walk the same days as the app in the same order.
      const key = list === "upcoming" ? (id) => upcomingDay(actualByUUID.get(id)) : (id) => id;
      const index = expected.findIndex((id, position) => key(actual[position]) !== key(id));
      if (index !== -1) {
        findings.push({ type: "order", list, index, expected: expected[index], actual: actual[index] });
      }
    }
  }

  return { ok: findings.length === 0, counts, findings };
}

function shortID(id) {
  const value = String(id || "");
  return value.length > 8 ? `${value.slice(0, 8)}...` : value;
}

function renderResult(result) {
  const lines = OPEN_LISTS.map((list) => {
    const count = result.counts[list];
    return `${list.padEnd(8)} Things ${String(count.expected).padStart(4)}  App ${String(count.actual).padStart(4)}`;
  });

  if (result.ok) {
    lines.push("", "PASS: list membership, completion state, and ordering match Things.");
    return lines.join("\n");
  }

  lines.push("", `FAIL: ${result.findings.length} parity mismatch(es).`);
  for (const finding of result.findings) {
    if (finding.type === "order") {
      lines.push(
        `- ${finding.list}: order differs at position ${finding.index + 1} (Things ${shortID(finding.expected)}, App ${shortID(finding.actual)}).`,
      );
      continue;
    }
    const ids = finding.uuids.slice(0, 5).map(shortID).join(", ");
    const remainder = finding.uuids.length > 5 ? ` and ${finding.uuids.length - 5} more` : "";
    lines.push(`- ${finding.list}: ${finding.type} ${finding.uuids.length} task(s) [${ids}${remainder}].`);
  }
  return lines.join("\n");
}

function parseArguments(argv) {
  const options = { url: "http://127.0.0.1:4173" };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--reference" || argument === "--actual" || argument === "--url") {
      const value = argv[index + 1];
      if (!value) throw new Error(`${argument} requires a value.`);
      options[argument.slice(2)] = value;
      index += 1;
      continue;
    }
    throw new Error(`Unknown argument: ${argument}`);
  }
  if (!options.reference) throw new Error("--reference is required.");
  return options;
}

async function readJSON(path) {
  return JSON.parse(await fs.readFile(path, "utf8"));
}

function appRequestHeaders(environment = process.env) {
  const headers = { accept: "application/json" };
  const password = environment.PARITY_APP_PASSWORD;
  if (password) {
    const username = environment.PARITY_APP_USERNAME || "admin";
    headers.authorization = `Basic ${Buffer.from(`${username}:${password}`).toString("base64")}`;
  }
  return headers;
}

async function fetchAppModel(baseURL) {
  const endpoint = new URL("/api/sync", baseURL);
  const response = await fetch(endpoint, { headers: appRequestHeaders() });
  const text = await response.text();
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error(`App returned non-JSON from ${endpoint} (HTTP ${response.status}).`);
  }
  if (!response.ok) throw new Error(data.error || `App sync failed with HTTP ${response.status}.`);
  return data;
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const reference = await readJSON(options.reference);
  const model = options.actual ? await readJSON(options.actual) : await fetchAppModel(options.url);
  const result = compareManifests(reference, model);
  console.log(renderResult(result));
  if (!result.ok) process.exitCode = 1;
}

if (require.main === module) {
  main().catch((error) => {
    console.error(`Parity check failed: ${error.message}`);
    process.exitCode = 2;
  });
}

module.exports = { appRequestHeaders, compareManifests, renderResult };
