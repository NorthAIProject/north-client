/**
 * Alpine wrapper for the body figure (NOR-8).
 *
 * Registers `northMuscleViewer(props)`, where props come from
 * web/shared/muscleviewer/muscleviewer.templ:
 *
 *   heat     region key → 0..1, every colour already decided by the server
 *   details  region key → {name, last?, href?}, already in the reader's language
 *   inspect  whether a tap opens the callout with "last trained" and a link
 *
 * Loading is driven by IntersectionObserver, not by whoever embeds this
 * component: a closed native <dialog> has no box, so its canvas is never
 * intersecting until the dialog opens — the same mechanism handles "load on
 * first open" and "free the WebGL context on close".
 *
 * When the 3D figure cannot start (no WebGL), `failed` shows the flat SVG
 * figure the templ already rendered. Its regions call select() directly.
 *
 * Alpine is loaded with `defer` (web/shared/layout/base.templ), and this
 * script is a plain tag, so document order alone guarantees it registers
 * before alpine:init fires.
 */
(function () {
  "use strict";

  // Reuse this script's own cache-bust query for the imported module, so an
  // immutable-cached asset doesn't mask a rebuild.
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
      selected: null,
      heat: props.heat || {},
      details: props.details || {},
      inspect: Boolean(props.inspect),

      init() {
        this.observer = new IntersectionObserver(
          (entries) => {
            if (entries.some((e) => e.isIntersecting)) this.load();
            else this.teardown();
          },
          { rootMargin: "300px" },
        );
        this.observer.observe(this.$el);
      },

      async load() {
        if (this.viewer || this.ready) return;
        try {
          const module = await import(viewerModuleURL);
          this.viewer = await module.createViewer(this.$refs.canvas, {
            reduced,
            dark: document.documentElement.classList.contains("dark"),
            onMuscleClick: (key) => this.select(key),
          });
          this.viewer.setHeat(this.heat);
        } catch (err) {
          this.failed = true;
        }
        this.ready = true;
      },

      // A tap on a region, from the canvas or the flat figure. On a figure
      // that only shows what an exercise works, the callout is just the name.
      select(key) {
        const detail = key && this.details[key];
        if (!detail) {
          this.clear();
          return;
        }
        this.selected = this.inspect ? { key, ...detail } : { key, name: detail.name };
        if (this.viewer) this.viewer.select(key);
      },

      clear() {
        this.selected = null;
        if (this.viewer) this.viewer.select(null);
      },

      // Frees the WebGL context but keeps the props, so scrolling or opening
      // the dialog again reloads clean.
      teardown() {
        if (this.viewer) this.viewer.destroy();
        this.viewer = null;
        this.ready = false;
        this.selected = null;
      },

      destroy() {
        if (this.observer) this.observer.disconnect();
        this.teardown();
      },
    }));
  });
})();
