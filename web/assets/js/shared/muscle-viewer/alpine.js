/**
 * Alpine wrapper for the shared muscle viewer (NOR-8).
 *
 * Registers `northMuscleViewer(props)` — `props` is
 * `{primary: string[], secondary: string[], stabilizers: string[], exerciseName: string}`,
 * matching web/shared/muscleviewer/muscleviewer.templ's Props.
 *
 * Loading is driven entirely by IntersectionObserver, not by whoever embeds
 * this component: a closed native <dialog> has no box (display:none per the
 * UA stylesheet), so its canvas is never intersecting until the dialog opens
 * — the same mechanism handles "load on first open" and "free the WebGL
 * context on close" without either caller needing to know about the other.
 *
 * Alpine is loaded with `defer` (web/shared/layout/base.templ), and this
 * script is a plain (non-deferred, non-module) tag, so document order alone
 * guarantees it registers before alpine:init fires — same technique
 * web/landing/scripts.templ uses for northMuscle.
 */
(function () {
  "use strict";

  // Mirrors landing.js's assetURL(): reuse this script's own cache-bust query
  // param for the dynamically imported module, so an immutable-cached asset
  // doesn't mask a rebuild.
  function assetURL(path) {
    const src = document.currentScript && document.currentScript.src;
    const version = src ? new URL(src, location.href).searchParams.get("v") : null;
    return version ? `${path}?v=${version}` : path;
  }

  const viewerModuleURL = assetURL("/assets/js/shared/muscle-viewer/viewer.js");
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  document.addEventListener("alpine:init", () => {
    window.Alpine.data("northMuscleViewer", (props = {}) => ({
      ready: false,
      failed: false,
      viewer: null,
      observer: null,
      selectedMuscle: null,
      primary: props.primary || [],
      secondary: props.secondary || [],
      stabilizers: props.stabilizers || [],
      exerciseName: props.exerciseName || "",

      init() {
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
        if (this.viewer || this.ready) return;
        try {
          const module = await import(viewerModuleURL);
          this.viewer = await module.createViewer(this.$refs.canvas, {
            reduced,
            dark: true, // /app/* is permanently dark today — see plan.md
            onMuscleClick: (key, meta) => {
              this.selectedMuscle = meta ? { key, ...meta } : null;
            },
          });
          this.ready = true;
          this.viewer.setMuscleGroups({
            primary: this.primary,
            secondary: this.secondary,
            stabilizers: this.stabilizers,
          });
        } catch (err) {
          this.failed = true;
          this.ready = true;
        }
      },

      // Frees the WebGL context without discarding the component's own state
      // (props survive), so scrolling/closing back into view reloads clean.
      teardown() {
        if (this.viewer) this.viewer.destroy();
        this.viewer = null;
        this.ready = false;
        this.failed = false;
        this.selectedMuscle = null;
      },

      destroy() {
        if (this.observer) this.observer.disconnect();
        this.teardown();
      },
    }));
  });
})();
