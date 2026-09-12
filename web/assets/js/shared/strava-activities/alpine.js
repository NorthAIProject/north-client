/**
 * Alpine wrapper for the calendar terrain.
 *
 * Registers `northActivityTerrain`. The first page of terrain is
 * server-rendered into a JSON script tag rather than fetched, so the scene
 * draws without a round trip and the page still works when the paging
 * endpoint is down. Later pages arrive from /app/fitness/activities/terrain.
 *
 * This file owns state and paging. It does not own the scene: everything
 * three.js lives behind a dynamic import, so a browser without WebGL, a
 * phone, or a failed module download all end up on the same path — the strip
 * of weeks below carries every fact the landscape shows.
 */
(function () {
  "use strict";

  function assetURL(path) {
    const src = document.currentScript && document.currentScript.src;
    const version = src ? new URL(src, location.href).searchParams.get("v") : null;
    return version ? `${path}?v=${version}` : path;
  }

  const sceneModuleURL = assetURL("/assets/js/shared/strava-activities/scene.js");
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Below this width the scene is not built at all. The landing page's scroll
  // world already draws this line and states why: a phone has neither the
  // pixels to read a receding landscape nor the patience for the download.
  // 640 rather than that file's 768, because unlike the landing world this is
  // the page's content and a tablet should get it.
  const minSceneWidth = 640;

  document.addEventListener("alpine:init", () => {
    window.Alpine.data("northActivityTerrain", () => ({
      ready: false,
      failed: false,
      scene: null,

      // weeks is every page fetched so far, oldest first. Kept even when the
      // scene disposes a week's geometry, so travelling back toward now never
      // re-requests anything.
      weeks: [],
      hasOlder: false,
      nextBefore: null,
      loadScale: 0,

      selected: null,
      hovered: null,
      loading: false,

      init() {
        const payload = document.getElementById("activity-terrain-data");
        let data = null;
        try {
          data = payload ? JSON.parse(payload.textContent) : null;
        } catch {
          data = null;
        }

        if (!data || !Array.isArray(data.weeks) || data.weeks.length === 0) {
          this.ready = true; // nothing to draw; the page says so in HTML
          return;
        }

        this.weeks = data.weeks;
        this.hasOlder = Boolean(data.has_older);
        this.nextBefore = data.next_before || null;
        this.loadScale = data.load_scale_met_min || 0;

        if (window.innerWidth < minSceneWidth) {
          this.ready = true;
          this.failed = true;
          return;
        }

        const canvas = this.$refs.canvas;
        if (!canvas) {
          this.ready = true;
          this.failed = true;
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
        observer.observe(canvas);
      },

      async load() {
        try {
          const module = await import(sceneModuleURL);
          this.scene = await module.createScene(this.$refs.canvas, {
            weeks: this.weeks,
            loadScale: this.loadScale,
            labels: this.$refs.labels,
            reduced,
            onSelect: (date) => {
              this.selected = date;
              this.scrollToDay(date);
            },
            onHover: (day, position) => this.showTooltip(day, position),
            onNeedOlder: () => this.loadOlder(),
          });
          this.ready = true;
        } catch {
          // Losing the scene costs presentation, not information.
          this.failed = true;
          this.ready = true;
        }
      },

      // Paging. One request in flight at a time: travelling fast must not open
      // six of them, and a failure stops asking rather than spinning.
      async loadOlder() {
        if (this.loading || !this.hasOlder || !this.nextBefore) return;
        this.loading = true;

        try {
          const response = await fetch(
            `/app/fitness/activities/terrain?before=${encodeURIComponent(this.nextBefore)}`,
            { headers: { Accept: "application/json" } },
          );
          if (!response.ok) throw new Error(`terrain page: ${response.status}`);

          const data = await response.json();
          const older = Array.isArray(data.weeks) ? data.weeks : [];

          this.weeks = older.concat(this.weeks);
          this.hasOlder = Boolean(data.has_older);
          this.nextBefore = data.next_before || null;

          if (this.scene) this.scene.prepend(older);
        } catch {
          // Stop travelling rather than retrying forever. The strip's own
          // "load earlier weeks" link still works.
          this.hasOlder = false;
        } finally {
          this.loading = false;
        }
      },

      pick(date) {
        this.selected = date;
        if (this.scene) this.scene.select(date);
      },

      isSelected(date) {
        return this.selected === date;
      },

      // Each row carries the day it belongs to. This used to search the markup
      // for an Alpine attribute containing the date, which tied the scene's
      // scrolling to the spelling of a template.
      //
      // The day may be on another page of the list, in which case there is
      // nothing to scroll to and the selection simply shows on the terrain.
      scrollToDay(date) {
        const row = document.querySelector(`[data-day="${date}"]`);
        if (row) row.scrollIntoView({ block: "nearest" });
      },

      showTooltip(day, position) {
        this.hovered = day;
        const tooltip = this.$refs.tooltip;
        if (!tooltip || !day || !position) return;

        // Clamped inside the canvas so a day at the edge does not push the
        // readout off it.
        const bounds = this.$refs.canvas.getBoundingClientRect();
        const x = Math.min(Math.max(position.x + 12, 8), bounds.width - tooltip.offsetWidth - 8);
        const y = Math.min(Math.max(position.y + 12, 8), bounds.height - tooltip.offsetHeight - 8);
        tooltip.style.transform = `translate(${x}px, ${y}px)`;
      },

      daySummary(day) {
        if (!day) return "";
        const parts = [`${Math.round(day.load_met_min)} MET-min`];
        if (day.sessions > 0) {
          parts.push(day.sessions === 1 ? "1 session" : `${day.sessions} sessions`);
        }
        if (day.distance_m > 0) parts.push(`${(day.distance_m / 1000).toFixed(1)} km`);
        if (day.elevation_m > 0) parts.push(`${Math.round(day.elevation_m)} m climb`);
        return parts.join(" · ");
      },

      mixSummary(day) {
        if (!day || !Array.isArray(day.mix)) return "";
        return day.mix.map((share) => share.family).join(" + ");
      },

      destroy() {
        if (this.scene) this.scene.destroy();
      },
    }));
  });
})();
