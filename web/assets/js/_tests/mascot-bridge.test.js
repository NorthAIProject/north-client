// Tests for the chat status wiring in the mascot bridge
// (web/assets/js/shared/mascot/alpine.js): which header phase and status line
// each stream event produces, per _reviews/muse-chat-contract.md.
//
// Run with `task test:js`. Loads the real shipped file into a stub browser,
// the same way command-palette.test.js does, then fires the events htmx's SSE
// extension and the composer would fire and reads what landed on the
// #chat-root scope.

const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const SOURCE = path.join(__dirname, "..", "shared", "mascot", "alpine.js");

// The copy #chat-root carries as data-status-* (web/chat/chat.templ).
const COPY = {
  statusReady: "Ready",
  statusListening: "is listening",
  statusThinking: "is thinking",
  statusWriting: "is writing",
  statusSnag: "hit a snag",
};

function load({ reduced = false } = {}) {
  const listeners = {};
  const timers = [];
  let component = null;

  const sandbox = {
    window: {
      matchMedia: () => ({ matches: reduced }),
      Alpine: {
        data(_name, definition) {
          component = definition;
        },
      },
    },
    document: {
      addEventListener(type, fn) {
        (listeners[type] = listeners[type] || []).push(fn);
      },
    },
    setTimeout(fn, ms) {
      const timer = { fn, ms, cleared: false };
      timers.push(timer);
      return timer;
    },
    clearTimeout(timer) {
      if (timer) timer.cleared = true;
    },
    Date,
  };
  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(SOURCE, "utf8"), sandbox);

  const fire = (type, detail = {}, target = null) => {
    for (const fn of listeners[type] || []) fn({ type, detail, target });
  };
  fire("alpine:init");

  // A #chat-root scope, bound the way chatRootData's init binds it.
  const scope = { status: "Ready", phase: "idle" };
  sandbox.window.NorthMascot.bindChat(scope, { dataset: COPY });

  // One mounted mascot, so pose changes can be observed too.
  const mascot = component({ id: "chat-mascot" });
  mascot.init();

  return {
    api: sandbox.window.NorthMascot,
    scope,
    mascot,
    timers,
    fire,
    message: (event, data = "") => fire("htmx:sse:after:message", { message: { event, data } }),
    composer: { closest: (sel) => (sel === "[data-muse-listen]" ? {} : null) },
  };
}

test("idle reads Ready with no ring", () => {
  const { scope } = load();
  assert.strictEqual(scope.status, "Ready");
  assert.strictEqual(scope.phase, "idle");
});

test("focusing the composer is listening; leaving it is idle again", () => {
  const { scope, mascot, fire, composer } = load();

  fire("focusin", {}, composer);
  assert.strictEqual(scope.phase, "listening");
  assert.strictEqual(scope.status, "is listening");
  assert.strictEqual(mascot.state, "listening");

  fire("focusout", {}, composer);
  assert.strictEqual(scope.phase, "idle");
  assert.strictEqual(scope.status, "Ready");
});

test("focus elsewhere on the page does not claim Khepri is listening", () => {
  const { scope, fire } = load();
  fire("focusin", {}, { closest: () => null });
  assert.strictEqual(scope.phase, "idle");
});

test("an open stream is working: thinking, then writing once tokens arrive", () => {
  const { scope, mascot, fire, message } = load();

  fire("htmx:sse:after:connection");
  assert.strictEqual(scope.phase, "working");
  assert.strictEqual(scope.status, "is thinking");
  assert.strictEqual(mascot.state, "working");

  message("", "Hello");
  assert.strictEqual(scope.phase, "working");
  assert.strictEqual(scope.status, "is writing");
});

test("a status frame shows the tool line, and the next token goes back to writing", () => {
  const { scope, fire, message } = load();

  fire("htmx:sse:after:connection");
  message("status", "is checking your goals");
  assert.strictEqual(scope.phase, "working");
  assert.strictEqual(scope.status, "is checking your goals");

  message("", "You have two.");
  assert.strictEqual(scope.status, "is writing");
});

test("focus changes nothing while a reply is in flight", () => {
  const { scope, fire, composer } = load();
  fire("htmx:sse:after:connection");
  fire("focusin", {}, composer);
  assert.strictEqual(scope.phase, "working");
  assert.strictEqual(scope.status, "is thinking");
});

test("done celebrates for 1.2s, then idles", () => {
  const { api, scope, mascot, timers, fire, message } = load();

  fire("htmx:sse:after:connection");
  message("", "Done.");
  fire("htmx:sse:close");

  assert.strictEqual(scope.phase, "celebrating");
  assert.strictEqual(scope.status, "Ready");
  assert.strictEqual(mascot.state, "celebrate");
  assert.strictEqual(api.state, "idle", "the sustained pose under the gesture is idle");

  const timer = timers.find((t) => !t.cleared);
  assert.strictEqual(timer.ms, 1200);
  timer.fn();
  assert.strictEqual(scope.phase, "idle");
  assert.strictEqual(scope.status, "Ready");
});

// The "done" refresh swaps #chat-root; the new scope must adopt the beat that
// is still playing rather than start from its server-rendered Ready.
test("a #chat-root that mounts mid-celebration adopts it", () => {
  const { api, fire, message } = load();
  fire("htmx:sse:after:connection");
  message("", "Done.");
  fire("htmx:sse:close");

  const fresh = { status: "Ready", phase: "idle" };
  api.bindChat(fresh, { dataset: COPY });
  assert.strictEqual(fresh.phase, "celebrating");
});

test("a failed turn hits a snag and keeps saying so through the refresh", () => {
  const { api, scope, mascot, fire, message } = load();

  fire("htmx:sse:after:connection");
  message("failed");
  message("", "<p>Something went wrong</p>");
  assert.strictEqual(scope.phase, "failed");
  assert.strictEqual(scope.status, "hit a snag");
  assert.strictEqual(mascot.state, "idle");

  fire("htmx:sse:close");
  assert.strictEqual(scope.phase, "failed", "done after a failure is not a celebration");

  const fresh = { status: "Ready", phase: "idle" };
  api.bindChat(fresh, { dataset: COPY });
  assert.strictEqual(fresh.status, "hit a snag");
});

test("a connection error is a snag too", () => {
  const { scope, fire } = load();
  fire("htmx:sse:after:connection");
  fire("htmx:sse:error");
  assert.strictEqual(scope.phase, "failed");
  assert.strictEqual(scope.status, "hit a snag");
});

test("focusing the composer after a snag clears it", () => {
  const { scope, fire, message, composer } = load();
  fire("htmx:sse:after:connection");
  message("failed");
  fire("focusin", {}, composer);
  assert.strictEqual(scope.status, "is listening");
});

test("a new turn during the celebration cancels its timer", () => {
  const { scope, timers, fire, message } = load();
  fire("htmx:sse:after:connection");
  message("", "x");
  fire("htmx:sse:close");
  fire("htmx:sse:after:connection");

  assert.ok(timers.every((t) => t.cleared), "the celebrate timer was not cleared");
  assert.strictEqual(scope.phase, "working");
});

test("reduced motion skips the gesture but the status still updates", () => {
  const { scope, mascot, fire, message } = load({ reduced: true });

  fire("htmx:sse:after:connection");
  message("status", "is checking your goals");
  assert.strictEqual(scope.status, "is checking your goals");

  fire("htmx:sse:close");
  assert.strictEqual(scope.phase, "celebrating");
  assert.notStrictEqual(mascot.state, "celebrate");
});

test("contract aliases resolve to poses", () => {
  const { api, mascot } = load();
  api.setState("celebrating");
  assert.strictEqual(mascot.state, "celebrate");
  api.setState("working");
  assert.strictEqual(api.state, "working");
  api.setState("failed");
  assert.strictEqual(api.state, "idle");
});
