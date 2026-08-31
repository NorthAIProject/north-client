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
 * In chat, `sse-close="done"` fires a trigger that re-GETs the page and swaps
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

  const SUSTAINED = ["idle", "thinking", "listening"];
  const GESTURES = ["celebrate", "nod"];

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

  /**
   * Drives every mounted mascot, or one by id.
   *
   * @param {string} name  idle | thinking | listening | celebrate | nod
   * @param {{id?: string}} [options]
   */
  function setState(name, options = {}) {
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

  window.NorthMascot = {
    setState,
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
  // Chat has no app-authored generation events: the stream is declarative htmx
  // (web/chat/chat.templ) and the server emits only token/error/done. So the
  // mascot listens to the events htmx already bubbles to document. Nothing
  // about the stream protocol changes, and no Go handler knows this exists.
  document.addEventListener("htmx:sseOpen", () => setState("thinking"));
  document.addEventListener("htmx:sseError", () => setState("idle"));
  document.addEventListener("htmx:sseClose", () => {
    setState("idle");
    setState("nod");
  });

  document.addEventListener("alpine:init", () => {
    window.Alpine.data("northMascot", (props = {}) => ({
      id: props.id || "",
      initial: isKnown(props.state) ? props.state : "idle",
      gesture: GESTURES.includes(props.gesture) ? props.gesture : "",
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
