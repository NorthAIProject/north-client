/**
 * Alpine wrapper and public state API for the Khepri companion (NOR-51).
 *
 * Registers `northMascot(props)` — `props` is `{state: string, id: string,
 * gesture: string}`, matching web/shared/mascot/mascot.templ's Props.
 *
 * The still is the mascot: alpine.js only writes `data-state` on the wrapper,
 * and CSS in input.css is what actually moves it. There is no WebGL path and
 * no lazy module import, so the app shell pays for a PNG and this file.
 *
 * The state lives on this module, not on the component. That is deliberate.
 * In chat, `hx-sse:close="done"` fires a trigger that re-GETs the page and swaps
 * `#chat-root` outerHTML — destroying the mascot at the exact moment the reply
 * finishes and the nod should play. An htmx swap does not reload this script,
 * so a mascot that mounts into the new DOM adopts the sustained state and
 * replays a gesture that was asked for a moment ago.
 *
 * Alpine is loaded with `defer` (web/shared/layout/base.templ) and this script
 * is a plain non-deferred tag, so document order alone guarantees registration
 * before alpine:init.
 */
(function () {
  "use strict";

  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // "working" is the chat contract's name for a reply in flight. It wears the
  // idle pose on purpose: there is no working art, and the ring drawn around
  // the header avatar (input.css, .muse-ring) is what says "busy".
  const SUSTAINED = ["idle", "thinking", "listening", "working"];
  const GESTURES = ["celebrate", "nod"];

  // The contract (_reviews/muse-chat-contract.md) names states by what Khepri
  // is doing; the poses were named first, by how it moves. Aliases let callers
  // use either without a second set of CSS.
  const ALIASES = { celebrating: "celebrate", failed: "idle" };

  // How long "celebrating" holds after a reply lands before the header goes
  // back to idle. From the contract.
  const CELEBRATE_MS = 1200;

  // How recently a gesture must have been asked for to be worth replaying on a
  // mascot that mounts after a swap. Long enough to survive the re-GET, short
  // enough that scrolling a mascot into view minutes later does not nod at you.
  const GESTURE_REPLAY_MS = 1000;

  const instances = new Set();
  let sustained = "idle";
  let lastGesture = null;
  let lastGestureAt = 0;

  function isKnown(name) {
    return SUSTAINED.includes(name) || GESTURES.includes(name);
  }

  function canonical(name) {
    return ALIASES[name] || name;
  }

  /**
   * Drives every mounted mascot, or one by id.
   *
   * @param {string} name  idle | thinking | listening | working | celebrate |
   *   nod, or a contract alias (celebrating, failed)
   * @param {{id?: string}} [options]
   */
  function setState(name, options = {}) {
    name = canonical(name);
    if (!isKnown(name)) return;

    if (GESTURES.includes(name)) {
      lastGesture = name;
      lastGestureAt = Date.now();
    } else {
      sustained = name;
    }

    for (const instance of instances) {
      if (options.id && instance.id !== options.id) continue;
      instance.apply(name);
    }
  }

  // --- chat status line -----------------------------------------------------
  //
  // The Muse header shows a phase (drives the ring) and a status line under
  // the name ("is thinking", "is checking your goals"). Both live here, on the
  // module, for the same reason the pose does: the "done" refresh swaps
  // #chat-root outerHTML, and the celebrating beat has to outlive the scope
  // that started it. The new #chat-root calls bindChat from its init and
  // adopts whatever this module currently believes.
  //
  // Copy comes from the page, never from this file: #chat-root carries the
  // translated lines as data-status-* attributes (web/chat/js.go). A tool line
  // arrives already translated in the SSE "status" frame.
  let phase = "idle";
  let statusKey = "ready";
  let statusLine = "";
  let chat = null; // { scope, copy }
  let celebrateTimer = null;

  function lineFor(key) {
    if (key === "tool") return statusLine;
    const copy = chat ? chat.copy : {};
    return copy[key] || copy.ready || "";
  }

  function render() {
    if (!chat) return;
    chat.scope.phase = phase;
    chat.scope.status = lineFor(statusKey);
  }

  /**
   * Moves the chat header to a phase and status line.
   *
   * @param {string} next  idle | listening | working | celebrating | failed
   * @param {string} key   ready | listening | thinking | writing | snag | tool
   * @param {string} [line] the translated line, for key "tool"
   */
  function setChat(next, key, line) {
    if (celebrateTimer && next !== "celebrating") {
      clearTimeout(celebrateTimer);
      celebrateTimer = null;
    }
    phase = next;
    statusKey = key;
    statusLine = line || "";
    render();
  }

  function bindChat(scope, el) {
    const data = (el && el.dataset) || {};
    chat = {
      scope,
      copy: {
        ready: data.statusReady || scope.status || "",
        listening: data.statusListening || "",
        thinking: data.statusThinking || "",
        writing: data.statusWriting || "",
        snag: data.statusSnag || "",
      },
    };
    render();
  }

  window.NorthMascot = {
    setState,
    bindChat,
    get phase() {
      return phase;
    },
    // Read-only view of what the module currently believes, for debugging from
    // the console and for tests.
    get state() {
      return sustained;
    },
  };

  // Lets a server-rendered HTMX response drive the mascot without inline JS.
  document.addEventListener("north:mascot-state", (event) => {
    const detail = event.detail || {};
    if (detail.state) setState(detail.state, { id: detail.id });
  });

  // --- coach stream bridge ---------------------------------------------------
  //
  // The stream is declarative htmx (web/chat/chat.templ). The server sends
  // unnamed content frames (tokens, the error panel) and three named signals:
  // "status" with a translated tool line, "failed" just before an error
  // panel, and "done". The bridge listens to what the SSE extension already
  // bubbles to document, so the page needs no inline JS for any of it.
  //
  //   connection open        → working, "is thinking"
  //   unnamed frame (token)  → working, "is writing"
  //   status                 → working, "is {tool}"
  //   failed / sse error     → failed, "hit a snag"
  //   close after working    → celebrating for 1.2s, then idle
  document.addEventListener("htmx:sse:after:connection", () => {
    setState("working");
    setChat("working", "thinking");
  });

  document.addEventListener("htmx:sse:after:message", (event) => {
    const message = (event.detail && event.detail.message) || {};
    switch (message.event || "") {
      case "":
        if (phase === "working") setChat("working", "writing");
        break;
      case "status":
        if (phase === "working" && message.data) setChat("working", "tool", message.data.trim());
        break;
      case "failed":
        setState("failed");
        setChat("failed", "snag");
        break;
    }
  });

  document.addEventListener("htmx:sse:error", () => {
    setState("failed");
    setChat("failed", "snag");
  });

  document.addEventListener("htmx:sse:close", () => {
    // A failed turn keeps saying so after the refresh; it clears the next time
    // the person does something (focuses the composer, sends again).
    if (phase !== "working") return;
    setState("idle");
    setState("celebrating");
    setChat("celebrating", "ready");
    celebrateTimer = setTimeout(() => {
      celebrateTimer = null;
      if (phase === "celebrating") setChat("idle", "ready");
    }, CELEBRATE_MS);
  });

  // Listening is the composer having focus. Delegated, because the composer
  // is re-rendered by every swap; the textarea opts in with data-muse-listen.
  function isComposer(target) {
    return Boolean(target && target.closest && target.closest("[data-muse-listen]"));
  }

  document.addEventListener("focusin", (event) => {
    if (!isComposer(event.target)) return;
    if (phase === "working") return;
    setState("listening");
    setChat("listening", "listening");
  });

  document.addEventListener("focusout", (event) => {
    if (!isComposer(event.target)) return;
    if (phase !== "listening") return;
    setState("idle");
    setChat("idle", "ready");
  });

  document.addEventListener("alpine:init", () => {
    window.Alpine.data("northMascot", (props = {}) => ({
      id: props.id || "",
      initial: isKnown(canonical(props.state)) ? canonical(props.state) : "idle",
      gesture: GESTURES.includes(canonical(props.gesture)) ? canonical(props.gesture) : "",
      state: "idle",

      init() {
        // A mascot that declares a sustained state is also saying what the page
        // means — onboarding is listening, an empty dashboard is idle — so it
        // sets the module state rather than diverging from it.
        if (SUSTAINED.includes(this.initial)) {
          sustained = this.initial;
          this.state = this.initial;
        } else {
          this.state = sustained;
        }

        instances.add(this);

        if (reduced) return;

        // A greeting belongs to the page that rendered it, so it plays on
        // this mascot alone and is never broadcast.
        if (this.gesture) {
          this.state = this.gesture;
        } else if (lastGesture && Date.now() - lastGestureAt < GESTURE_REPLAY_MS) {
          // Otherwise catch up with anything that happened while this mascot
          // did not exist — the chat swap case described at the top.
          this.state = lastGesture;
        }
      },

      apply(name) {
        name = canonical(name);
        if (!isKnown(name)) return;
        if (reduced && GESTURES.includes(name)) return;
        this.state = name;
      },

      onAnimationEnd(event) {
        if (event.target !== this.$refs.img) return;
        if (!GESTURES.includes(this.state)) return;
        // A cancelled hop can fire animationend after a newer pose has already
        // taken data-state. Only hand back when this event belongs to the
        // pose that is currently showing.
        if (event.animationName !== "khepri-" + this.state) return;
        this.state = sustained;
      },

      destroy() {
        instances.delete(this);
      },
    }));
  });
})();
