/**
 * Behaviour for the marketing page.
 *
 * Everything here is presentation. The demos are canned so a visitor can
 * operate them before signing up; the real equivalents live behind /app.
 *
 * Alpine is loaded with `defer`, and so is this file, so document order
 * guarantees these definitions are registered before Alpine boots on
 * DOMContentLoaded.
 */
(function () {
  "use strict";

  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  // The demos' words come from the server's catalogue, rendered as JSON just
  // above this script (scriptCopy in web/landing/scripts.templ), so a page in
  // Spanish structures a goal in Spanish. tr fills {placeholders}; a missing
  // key renders as itself, which is visibly wrong rather than silently English.
  const copy = (() => {
    const el = document.getElementById("north-landing-copy");
    try {
      return el ? JSON.parse(el.textContent) : {};
    } catch (err) {
      console.error("[landing] copy unreadable", err);
      return {};
    }
  })();

  function tr(key, vars) {
    let s = copy[key] || key;
    if (vars) {
      for (const [name, value] of Object.entries(vars)) s = s.split(`{${name}}`).join(String(value));
    }
    return s;
  }

  // A plan line can open with a placeholder ("{pull}, press…"), which is a
  // lower-case word in every language.
  const capitalise = (s) => s.charAt(0).toUpperCase() + s.slice(1);

  // The page script is cache-busted by the server; reuse the same query for the
  // lazily imported module so a rebuild is not masked by an immutable header.
  function assetURL(path) {
    const src = document.currentScript && document.currentScript.src;
    const version = src ? new URL(src, location.href).searchParams.get("v") : null;
    return version ? `${path}?v=${version}` : path;
  }

  const viewerModuleURL = assetURL("/assets/js/shared/muscle-viewer/viewer.js");

  // ---------------------------------------------------------------------------
  // Page progress
  // ---------------------------------------------------------------------------

  /** How far down the page we are, 0..1. The bearing rail renders it. */
  const pageProgress = (() => {
    const subscribers = new Set();
    let value = 0;

    const update = () => {
      const max = document.documentElement.scrollHeight - window.innerHeight;
      value = max > 0 ? Math.min(1, window.scrollY / max) : 0;
      subscribers.forEach((fn) => fn(value));
    };

    window.addEventListener("scroll", update, { passive: true });
    window.addEventListener("resize", update, { passive: true });
    update();

    return {
      subscribe(fn) {
        subscribers.add(fn);
        fn(value);
        return () => subscribers.delete(fn);
      },
    };
  })();

  // templUI's tabs component tracks state in data attributes. ARIA needs to say
  // the same thing, so mirror it whenever the state changes.
  function syncTabsAria() {
    document.querySelectorAll("[data-tui-tabs-trigger][role='tab']").forEach((el) => {
      el.setAttribute(
        "aria-selected",
        el.getAttribute("data-tui-tabs-state") === "active" ? "true" : "false",
      );
    });
  }
  document.addEventListener("click", () => setTimeout(syncTabsAria, 0));

  document.addEventListener("alpine:init", () => {
    const Alpine = window.Alpine;

    // -----------------------------------------------------------------------
    // Bearing line
    // -----------------------------------------------------------------------
    // The rail renders the number; pageProgress above computes it.
    Alpine.data("northBearing", () => ({
      progress: 0,
      unsubscribe: null,

      init() {
        this.unsubscribe = pageProgress.subscribe((p) => {
          this.progress = p * 100;
        });
      },

      destroy() {
        if (this.unsubscribe) this.unsubscribe();
      },
    }));

    // -----------------------------------------------------------------------
    // Muscle viewer
    // -----------------------------------------------------------------------
    Alpine.data("northMuscle", () => ({
      ready: false,
      failed: false,
      viewer: null,
      themeHandler: null,
      root: null,

      init() {
        // Captured once, synchronously: $el resolves correctly here and in load()
        // (both run in Alpine's own call chain), but is unreliable from inside the
        // tabs library's click handler further down — see select() below.
        this.root = this.$el;
        const observer = new IntersectionObserver(
          (entries) => {
            if (!entries.some((e) => e.isIntersecting)) return;
            observer.disconnect();
            this.load();
          },
          { rootMargin: "300px" },
        );
        observer.observe(this.root);
      },

      async load() {
        try {
          const module = await import(viewerModuleURL);
          this.viewer = await module.createViewer(this.$refs.canvas, {
            reduced,
            dark: document.documentElement.classList.contains("dark"),
          });
          this.ready = true;
          this.apply();
          // The theme switcher announces itself; the figure has to follow or it
          // turns into a silhouette on a light panel. Handler is stored so destroy()
          // can remove it — the viewer now owns real GPU resources, not just DOM.
          this.themeHandler = () => {
            this.viewer.setTheme(document.documentElement.classList.contains("dark"));
          };
          document.addEventListener("theme-changed", this.themeHandler);
        } catch (err) {
          // The readout beside the canvas carries every fact the model shows,
          // so a failure here costs presentation rather than information.
          console.error("[landing] muscle viewer failed to load", err);
          this.failed = true;
          this.ready = true;
        }
      },

      destroy() {
        if (this.themeHandler) document.removeEventListener("theme-changed", this.themeHandler);
        if (this.viewer) this.viewer.destroy();
      },

      // Loads are read back out of the active panel so the numbers a visitor
      // reads and the colours they see cannot drift apart.
      activeLoads() {
        const panel = this.root.querySelector(
          "[data-north-lift][data-tui-tabs-state='active']",
        );
        if (!panel) return [];
        return Array.from(panel.querySelectorAll("[data-north-muscle]")).map((row) => ({
          key: row.dataset.northMuscle,
          share: Number(row.dataset.northShare),
          role: row.dataset.northRole,
        }));
      },

      apply() {
        if (this.viewer) this.viewer.setLoads(this.activeLoads());
      },

      select() {
        // The tabs library swaps data-tui-tabs-state synchronously on its own
        // document-level click listener, which fires after this one (bubble order),
        // so a deferred read is still needed to go after it — setTimeout(fn, 0),
        // same as syncTabsAria above needs for the same library.
        setTimeout(() => this.apply(), 0);
      },
    }));

    // -----------------------------------------------------------------------
    // Goal repair
    // -----------------------------------------------------------------------
    const blankGoal = () => ({
      objective: "",
      horizon: "",
      commitment: "",
      equipment: "",
      constraint: "",
      sessions: [],
      measure: "",
    });

    Alpine.data("northGoal", () => ({
      input: tr("land.goal.sample"),
      running: false,
      out: blankGoal(),

      init() {
        this.out = parseGoal(this.input);
      },

      reset() {
        this.running = false;
        this.out = parseGoal(this.input);
      },

      async run() {
        if (this.running) return;
        const parsed = parseGoal(this.input);
        this.out = blankGoal();

        if (reduced) {
          this.out = parsed;
          return;
        }

        this.running = true;
        for (const field of ["objective", "horizon", "commitment", "equipment", "constraint"]) {
          await this.type(field, parsed[field]);
          await sleep(90);
        }
        for (const session of parsed.sessions) {
          this.out.sessions.push(session);
          await sleep(130);
        }
        await sleep(160);
        this.out.measure = parsed.measure;
        this.running = false;
      },

      async type(field, value) {
        for (let i = 1; i <= value.length; i += 2) {
          this.out[field] = value.slice(0, i);
          await sleep(8);
        }
        this.out[field] = value;
      },
    }));

    // -----------------------------------------------------------------------
    // Cross-surface continuity
    // -----------------------------------------------------------------------
    Alpine.data("northSurfaces", () => ({
      order: ["web", "telegram", "discord", "whatsapp"],
      index: 0,
      timer: null,
      halted: false,

      init() {
        if (reduced) return;
        const observer = new IntersectionObserver(
          (entries) => {
            if (entries.some((e) => e.isIntersecting)) this.start();
            else this.stop();
          },
          { threshold: 0.35 },
        );
        observer.observe(this.$el);
      },

      start() {
        if (this.halted || this.timer) return;
        this.timer = setInterval(() => this.advance(), 4600);
      },

      stop() {
        clearInterval(this.timer);
        this.timer = null;
      },

      // Any deliberate interaction ends the tour for good — a panel that keeps
      // moving under a reader is worse than one that never moved.
      pause() {
        this.halted = true;
        this.stop();
      },

      advance() {
        this.index = (this.index + 1) % this.order.length;
        if (window.tui && window.tui.tabs) {
          window.tui.tabs.setActive("surface-tabs", this.order[this.index]);
          syncTabsAria();
        }
      },
    }));

    // -----------------------------------------------------------------------
    // BMI
    // -----------------------------------------------------------------------
    Alpine.data("northBmi", () => ({
      height: 178,
      weight: 82,

      sync() {
        const read = (id) => {
          const input = document.querySelector(`#${id} [data-input]`);
          return input ? Number(input.value) : null;
        };
        const h = read("bmi-height");
        const w = read("bmi-weight");
        if (h) this.height = h;
        if (w) this.weight = w;
      },

      get raw() {
        return this.weight / Math.pow(this.height / 100, 2);
      },

      get bmi() {
        return this.raw.toFixed(1).replace(".", tr("land.bmi.decimal"));
      },

      get bandKey() {
        if (this.raw < 18.5) return "under";
        if (this.raw < 25) return "normal";
        if (this.raw < 30) return "over";
        return "obese";
      },

      get band() {
        return tr(`land.bmi.band.${this.bandKey}`);
      },

      get reading() {
        return tr(`land.bmi.read.${this.bandKey}`, { h: this.height, w: this.weight, n: this.bmi });
      },
    }));

    // -----------------------------------------------------------------------
    // Goal setter
    // -----------------------------------------------------------------------
    Alpine.data("northSetter", () => ({
      goal: "strength",
      days: 3,
      kit: { barbell: true, dumbbells: true, pullup: false, none: false },

      toggle(key, checked) {
        this.kit[key] = checked;
        // "Nothing at all" is a claim about the other boxes, so it settles them.
        if (key === "none" && checked) {
          this.kit.barbell = false;
          this.kit.dumbbells = false;
          this.kit.pullup = false;
          document.querySelectorAll("#kit-barbell, #kit-dumbbells, #kit-pullup").forEach((el) => {
            el.checked = false;
            el.setAttribute("data-state", "unchecked");
          });
        } else if (checked) {
          this.kit.none = false;
          const none = document.querySelector("#kit-none");
          if (none) {
            none.checked = false;
            none.setAttribute("data-state", "unchecked");
          }
        }
      },

      get owned() {
        const names = [];
        if (this.kit.barbell) names.push(tr("land.setter.owned.barbell"));
        if (this.kit.dumbbells) names.push(tr("land.setter.owned.dumbbells"));
        if (this.kit.pullup) names.push(tr("land.setter.owned.pullup"));
        return names.length ? names.join(tr("land.setter.owned.and")) : tr("land.setter.owned.none");
      },

      get summary() {
        const key = this.days === 1 ? "land.setter.summary.one" : "land.setter.summary.many";
        return tr(key, {
          days: this.days,
          objective: tr(`land.setter.obj.${this.goal}`),
          owned: this.owned,
        });
      },

      get split() {
        const heavy = tr(
          this.kit.barbell ? "land.goal.heavy.barbell" : this.kit.dumbbells ? "land.goal.heavy.dumbbell" : "land.goal.heavy.bodyweight",
        );
        const pull = tr(
          this.kit.pullup ? "land.goal.pull.pullups" : this.kit.dumbbells ? "land.goal.pull.rows" : "land.goal.pull.inverted",
        );

        // Six days per goal in the catalogue: land.setter.<goal>.1 … .6.
        const days = [];
        for (let n = 1; n <= Number(this.days); n++) {
          days.push({
            day: tr("land.setter.day", { n }),
            work: capitalise(tr(`land.setter.${this.goal}.${n}`, { heavy, pull })),
          });
        }
        return days;
      },
    }));
  });

  // -------------------------------------------------------------------------
  // Goal parsing
  // -------------------------------------------------------------------------

  /**
   * Turns a sentence somebody would actually type into the shape a training
   * block can be built from. Deliberately shallow — it exists so the demo
   * responds to what a visitor writes rather than replaying a fixed script.
   */
  function parseGoal(text) {
    const t = (text || "").toLowerCase();

    // The keyword lists cover the four languages the page is served in, so the
    // demo answers a visitor who types in their own.
    const explicit = t.match(/([1-7])\s*(?:x|times|days?|sessions?|veces|d[ií]as?|vezes|sesi[oó]n(?:es)?|sess(?:ão|ões)|treinos?)/);
    const days = explicit ? Number(explicit[1]) : 3;

    const kit = [];
    if (/barbell|squat rack|the gym|gym\b|gimnasio|gin[aá]sio|academia/.test(t)) kit.push("barbell");
    if (/dumbbell|db\b|mancuerna|halter/.test(t)) kit.push("dumbbells");
    if (/pull ?-?up|chin ?-?up|dominada|barra fixa|elevaç/.test(t)) kit.push("pullup");
    if (/kettlebell|kb\b|pesa rusa/.test(t)) kit.push("kettlebell");
    if (/band|el[aá]stic/.test(t)) kit.push("bands");
    if (!kit.length) kit.push("none");

    const wantsLean = /lose|leaner|lean|fat|weight off|slim|cut|perder|adelgaz|grasa|gordura|emagrec|secar|definir/.test(t);
    const wantsStrong = /strong|strength|lift|muscle|bigger|gains|fuert|fuerza|m[uú]sculo|forte|for[cç]a|massa/.test(t);
    const wantsEndurance = /run|hike|cycle|endurance|cardio|marathon|fitter|correr|corrida|senderismo|caminhad|trilha|resist[eê]ncia|marat[oó]n|maratona|bici/.test(t);

    let objective = tr("land.goal.obj.habit");
    if (wantsStrong && wantsLean) {
      objective = tr("land.goal.obj.both");
    } else if (wantsStrong) {
      objective = tr("land.goal.obj.strong");
    } else if (wantsLean) {
      objective = tr("land.goal.obj.lean");
    } else if (wantsEndurance) {
      objective = tr("land.goal.obj.endurance");
    }

    const hasBack = /back|espalda|costas|lumbar|lombar/.test(t);
    const hasKnee = /knee|rodilla|joelho/.test(t);
    const hasShoulder = /shoulder|hombro|ombro/.test(t);

    let constraint = tr("land.goal.con.none");
    if (hasBack) {
      constraint = tr("land.goal.con.back");
    } else if (hasKnee) {
      constraint = tr("land.goal.con.knee");
    } else if (hasShoulder) {
      constraint = tr("land.goal.con.shoulder");
    } else if (/if i'?m lucky|busy|no time|work|kids|travel|suerte|sin tiempo|trabajo|niños|hijos|viaj|sorte|sem tempo|trabalho|filhos|miúdos|crianças/.test(t)) {
      constraint = tr("land.goal.con.busy");
    }

    const heavy = tr(
      kit.includes("barbell") ? "land.goal.heavy.barbell" : kit.includes("dumbbells") ? "land.goal.heavy.dumbbell" : "land.goal.heavy.bodyweight",
    );
    const pull = tr(
      kit.includes("pullup") ? "land.goal.pull.pullups" : kit.includes("dumbbells") ? "land.goal.pull.rows" : "land.goal.pull.inverted",
    );
    const cautious = hasBack || hasKnee || hasShoulder;

    const sessions = [
      tr(cautious ? "land.goal.s1.cautious" : "land.goal.s1", { heavy, pull }),
      tr(cautious ? "land.goal.s2.cautious" : "land.goal.s2", { heavy }),
      tr(wantsLean ? "land.goal.s3.lean" : "land.goal.s3"),
    ]
      .slice(0, Math.max(2, Math.min(3, days)))
      .map((work, i) => ({ day: tr("land.goal.session", { n: i + 1 }), work: capitalise(work) }));

    return {
      objective,
      horizon: tr("land.goal.horizon"),
      commitment: tr(days === 1 ? "land.goal.commit.one" : "land.goal.commit.many", { n: days }),
      // "kettlebell" is borrowed unchanged in every language the page speaks.
      equipment: kit.map((k) => (k === "kettlebell" ? k : tr(`land.goal.kit.${k}`))).join(", "),
      constraint,
      sessions,
      measure: tr(wantsLean ? "land.goal.measure.lean" : "land.goal.measure"),
    };
  }
})();
