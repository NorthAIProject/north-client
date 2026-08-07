/**
 * Alpine wrapper for the 3D activity landscape.
 *
 * Registers `northStravaActivities`. The activity data is server-rendered
 * into a JSON script tag rather than fetched, so the page has everything it
 * needs in one request and the list beside the canvas and the shapes inside
 * it can never disagree.
 */
(function () {
  "use strict";

  function assetURL(path) {
    const src = document.currentScript && document.currentScript.src;
    const version = src ? new URL(src, location.href).searchParams.get("v") : null;
    return version ? `${path}?v=${version}` : path;
  }

  const viewerModuleURL = assetURL("/assets/js/shared/strava-activities/viewer.js");
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  document.addEventListener("alpine:init", () => {
    window.Alpine.data("northStravaActivities", () => ({
      ready: false,
      failed: false,
      viewer: null,
      activities: [],
      selected: null,

      init() {
        const payload = document.getElementById("strava-activities-data");
        try {
          this.activities = payload ? JSON.parse(payload.textContent) : [];
        } catch {
          this.activities = [];
        }

        if (this.activities.length === 0) {
          this.ready = true; // nothing to draw; the page says so in HTML
          return;
        }

        const observer = new IntersectionObserver(
          (entries) => {
            if (!entries.some((e) => e.isIntersecting)) return;
            observer.disconnect();
            this.load();
          },
          { rootMargin: "200px" },
        );
        observer.observe(this.$refs.canvas);
      },

      async load() {
        try {
          const module = await import(viewerModuleURL);
          this.viewer = await module.createViewer(this.$refs.canvas, this.activities, {
            reduced,
            onSelect: (activity) => {
              this.selected = activity;
            },
          });
          this.ready = true;
        } catch (err) {
          // The list beside the canvas carries every fact the scene shows, so
          // losing the scene costs presentation rather than information.
          this.failed = true;
          this.ready = true;
        }
      },

      // Selecting from the list drives the scene, so the two stay in step
      // whichever one the person used.
      pick(index) {
        if (this.viewer) this.viewer.select(index);
        else this.selected = this.activities[index];
      },

      isSelected(index) {
        return this.selected !== null && this.selected === this.activities[index];
      },

      destroy() {
        if (this.viewer) this.viewer.destroy();
      },
    }));
  });
})();
