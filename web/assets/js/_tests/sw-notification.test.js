// Tests for what the service worker does when a notification is tapped.
//
// Run with `task test:js`. It loads the real web/pwa/sw.js into a VM with just
// enough of a worker global to register its listeners, then exercises the two
// functions the notificationclick handler is built from, and the handler
// itself end to end.

const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const SOURCE = path.join(__dirname, "..", "..", "..", "pwa", "sw.js");
const ORIGIN = "https://khepri.test";

function loadWorker(clients) {
  const listeners = {};
  const self = {
    location: { origin: ORIGIN },
    clients,
    registration: { showNotification: async () => undefined },
    addEventListener: (name, fn) => {
      listeners[name] = fn;
    },
  };
  const context = vm.createContext({ self, URL, Promise, console });
  vm.runInContext(fs.readFileSync(SOURCE, "utf8"), context, { filename: SOURCE });
  return { context, listeners };
}

// A window as clients.matchAll returns it. controlled=false models a tab the
// worker does not control, whose navigate() rejects.
function fakeWindow(url, { controlled = true } = {}) {
  const w = {
    url,
    focused: false,
    navigatedTo: null,
    async focus() {
      w.focused = true;
      return w;
    },
    async navigate(target) {
      if (!controlled) throw new TypeError("not controlled");
      w.navigatedTo = target;
      w.url = target;
      return w;
    },
  };
  return w;
}

function fakeClients(windows) {
  const c = {
    opened: [],
    async matchAll() {
      return windows;
    },
    async openWindow(url) {
      c.opened.push(url);
    },
  };
  return c;
}

test("a tap follows the href the push carried", () => {
  const { context } = loadWorker(fakeClients([]));
  const href = "/app/nudges/11111111-2222-3333-4444-555555555555/open?from=push";
  assert.strictEqual(
    context.notificationTarget("", { href }, ORIGIN),
    ORIGIN + href,
  );
});

test("the Log it action opens capture regardless of the href", () => {
  const { context } = loadWorker(fakeClients([]));
  assert.strictEqual(
    context.notificationTarget("capture", { href: "/app/chat/abc" }, ORIGIN),
    ORIGIN + "/app/capture",
  );
});

test("no href, or an off-site one, lands on /app", () => {
  const { context } = loadWorker(fakeClients([]));
  assert.strictEqual(context.notificationTarget("", undefined, ORIGIN), ORIGIN + "/app");
  assert.strictEqual(context.notificationTarget("", {}, ORIGIN), ORIGIN + "/app");
  assert.strictEqual(
    context.notificationTarget("", { href: "https://evil.test/phish" }, ORIGIN),
    ORIGIN + "/app",
  );
  assert.strictEqual(
    context.notificationTarget("", { href: "//evil.test/x" }, ORIGIN),
    ORIGIN + "/app",
  );
});

test("an open window is focused and navigated, not a new one opened", async () => {
  const win = fakeWindow(ORIGIN + "/app/goals");
  const clients = fakeClients([win]);
  const { context } = loadWorker(clients);
  await context.openNotificationTarget(clients, ORIGIN + "/app/chat/abc");
  assert.ok(win.focused);
  assert.strictEqual(win.navigatedTo, ORIGIN + "/app/chat/abc");
  assert.deepStrictEqual(clients.opened, []);
});

test("with no window open, a new one is opened on the target", async () => {
  const clients = fakeClients([]);
  const { context } = loadWorker(clients);
  await context.openNotificationTarget(clients, ORIGIN + "/app/chat/abc");
  assert.deepStrictEqual(clients.opened, [ORIGIN + "/app/chat/abc"]);
});

test("a window the worker cannot navigate is skipped for the next one", async () => {
  const stray = fakeWindow(ORIGIN + "/", { controlled: false });
  const app = fakeWindow(ORIGIN + "/app");
  const clients = fakeClients([stray, app]);
  const { context } = loadWorker(clients);
  await context.openNotificationTarget(clients, ORIGIN + "/app/chat/abc");
  assert.strictEqual(stray.navigatedTo, null);
  assert.strictEqual(app.navigatedTo, ORIGIN + "/app/chat/abc");
  assert.deepStrictEqual(clients.opened, []);
});

test("when no window can be navigated, a new one is opened", async () => {
  const stray = fakeWindow(ORIGIN + "/", { controlled: false });
  const clients = fakeClients([stray]);
  const { context } = loadWorker(clients);
  await context.openNotificationTarget(clients, ORIGIN + "/app/chat/abc");
  assert.deepStrictEqual(clients.opened, [ORIGIN + "/app/chat/abc"]);
});

test("the click handler closes the notification and opens the thread link", async () => {
  const win = fakeWindow(ORIGIN + "/app");
  const clients = fakeClients([win]);
  const { listeners } = loadWorker(clients);
  let closed = false;
  let waited;
  const href = "/app/nudges/abc/open?from=push";
  listeners.notificationclick({
    action: "",
    notification: { data: { href }, close: () => (closed = true) },
    waitUntil: (p) => (waited = p),
  });
  assert.ok(closed, "notification was not closed");
  await waited;
  assert.strictEqual(win.navigatedTo, ORIGIN + href);
});
