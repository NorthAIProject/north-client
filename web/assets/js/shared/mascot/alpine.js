/**
 * Alpine wrapper and public state API for the mascot (NOR-51).
 *
 * Registers `northMascot(props)` — `props` is `{state: string, id: string}`,
 * matching web/shared/mascot/mascot.templ's Props.
 *
 * Loading is driven by IntersectionObserver exactly as the muscle viewer does
 * (shared/muscle-viewer/alpine.js): three is imported when a mascot nears the
 * viewport and the WebGL context is freed when it leaves, so the app shell
 * never pays for a scene nobody is looking at.
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

  // Mirrors muscle-viewer/alpine.js: reuse this script's own cache-bust param
  // for the dynamically imported module. It has to read document.currentScript,
  // which is why it is copied rather than imported.
  function assetURL(path) {
    const src = document.currentScript && document.currentScript.src;
    const version = src ? new URL(src, location.href).searchParams.get("v") : null;
    return version ? `${path}?v=${version}` : path;
  }

  const sceneModuleURL = assetURL("/assets/js/shared/mascot/scene.js");
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
      ready: false,
      failed: false,
      mascot: null,
      observer: null,
      id: props.id || "",
      initial: isKnown(props.state) ? props.state : "idle",

      init() {
        // A mascot that declares a sustained state is also saying what the page
        // means — onboarding is listening, an empty dashboard is idle — so it
        // sets the module state rather than diverging from it.
        if (SUSTAINED.includes(this.initial)) sustained = this.initial;

        instances.add(this);

        this.observer = new IntersectionObserver(
          (entries) => {
            const visible = entries.some((e) => e.isIntersecting);
            if (visible) this.load();
            else this.teardown();
          },
          { rootMargin: "300px" },
        );
        this.observer.observe(this.$refs.canvas);
      },

      async load() {
        if (this.mascot || this.ready) return;

        try {
          const module = await import(sceneModuleURL);
          this.mascot = module.createMascot(this.$refs.canvas, {
            reduced,
            state: sustained,
          });
          this.ready = true;

          // Catch up with anything that happened while this mascot did not
          // exist — the chat swap case described at the top of this file.
          if (lastGesture && Date.now() - lastGestureAt < GESTURE_REPLAY_MS) {
            this.mascot.setState(lastGesture);
          }
        } catch (err) {
          // No WebGL, or the module failed to load. The template shows the
          // static PNG; there is nothing to report and nothing to retry.
          this.failed = true;
          this.ready = true;
        }
      },

      apply(name) {
        if (this.mascot) this.mascot.setState(name);
      },

      // Frees the WebGL context without discarding component state, so
      // scrolling back into view reloads clean.
      teardown() {
        if (this.mascot) this.mascot.destroy();
        this.mascot = null;
        this.ready = false;
        this.failed = false;
      },

      destroy() {
        instances.delete(this);
        if (this.observer) this.observer.disconnect();
        this.teardown();
      },
    }));
  });
})();
