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

  // The page script is cache-busted by the server; reuse the same query for the
  // lazily imported module so a rebuild is not masked by an immutable header.
  function assetURL(path) {
    const src = document.currentScript && document.currentScript.src;
    const version = src ? new URL(src, location.href).searchParams.get("v") : null;
    return version ? `${path}?v=${version}` : path;
  }

  const viewerModuleURL = assetURL("/assets/js/shared/muscle-viewer/viewer.js");
  const scrollModuleURL = assetURL("/assets/js/landing/scroll.js");
  const worldModuleURL = assetURL("/assets/js/landing/scroll-world/world.js");

  // ---------------------------------------------------------------------------
  // Page progress
  // ---------------------------------------------------------------------------

  /**
   * How far down the page we are, as one number with one owner.
   *
   * The bearing rail reads it and so does the 3D world. A rail that says 60%
   * while the camera is somewhere else reads as a bug even though neither value
   * is wrong on its own.
   *
   * A plain scroll listener owns it until scroll.js loads, at which point
   * ScrollTrigger takes over and the listener is removed. A handover, not two
   * sources running side by side.
   */
  const pageProgress = (() => {
    const subscribers = new Set();
    let value = 0;
    let native = null;

    const publish = (v) => {
      value = v;
      subscribers.forEach((fn) => fn(v));
    };

    const fromWindow = () => {
      const max = document.documentElement.scrollHeight - window.innerHeight;
      publish(max > 0 ? Math.min(1, window.scrollY / max) : 0);
    };

    window.addEventListener("scroll", fromWindow, { passive: true });
    window.addEventListener("resize", fromWindow, { passive: true });
    native = fromWindow;
    fromWindow();

    return {
      subscribe(fn) {
        subscribers.add(fn);
        fn(value);
        return () => subscribers.delete(fn);
      },
      // Called once, by the scroll driver, when it is ready to be the source.
      adopt(register) {
        if (native) {
          window.removeEventListener("scroll", native);
          window.removeEventListener("resize", native);
          native = null;
        }
        register(publish);
      },
    };
  })();

  // ---------------------------------------------------------------------------
  // Scroll driver and 3D world
  // ---------------------------------------------------------------------------

  /**
   * Loads GSAP, Lenis, and the world after first paint.
   *
   * Deferred to an idle callback rather than started here: this file runs
   * synchronously before Alpine boots, and roughly 170KB of scroll machinery is
   * not something the first frame should wait behind. Everything it improves is
   * a scroll away by definition.
   */
  function bootScrollWorld() {
    const host = document.getElementById("scroll-world");
    if (!host) return;

    import(scrollModuleURL)
      .then(async (scrollModule) => {
        const driver = scrollModule.createScrollDriver({ reduced });
        pageProgress.adopt((publish) => driver.onProgress(publish));

        // WebGL is the part most likely to be unavailable, so it is imported
        // last: a browser that cannot render the world still gets the smooth
        // scrolling and the rail stays driven by ScrollTrigger.
        const worldModule = await import(worldModuleURL);
        const world = worldModule.createWorld(host, { reduced });
        if (!world) return;

        // Waypoints stand at the middle of each section's own scroll range, so
        // passing a section and lighting its mark happen at the same moment.
        world.setSections(driver.sections.map((s) => (s.start + s.end) / 2));

        driver.onProgress((p) => world.update(p, driver.muscle && driver.muscle.active));

        // The sections were measured before the world existed and before any
        // late layout shift; one refresh once everything has settled keeps the
        // marks on the line where the sections actually are.
        window.addEventListener("load", () => driver.refresh(), { once: true });
      })
      .catch(() => {
        // The page is complete without any of this. There is no degraded state
        // to render and nothing for a visitor to act on, so it stays quiet.
      });
  }

  if ("requestIdleCallback" in window) {
    requestIdleCallback(bootScrollWorld, { timeout: 2000 });
  } else {
    window.addEventListener("load", bootScrollWorld, { once: true });
  }

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
    // The rail renders the number; it does not compute it. pageProgress above is
    // the owner, so the rail and the 3D world can never disagree about where the
    // page is — including across the handover to ScrollTrigger.
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
      input:
        "i wanna get stronger idk maybe lose a bit too, gym like 3x if im lucky, home has dumbbells and a pullup bar. back goes out sometimes",
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
        return this.raw.toFixed(1);
      },

      get band() {
        if (this.raw < 18.5) return "Under";
        if (this.raw < 25) return "Normal";
        if (this.raw < 30) return "Over";
        return "Obese";
      },

      get reading() {
        const n = this.bmi;
        if (this.raw < 18.5) {
          return `At ${this.height}cm and ${this.weight}kg the index reads ${n}, which usually means eating is the training. Khepri would put food and sleep ahead of any programme for the first month.`;
        }
        if (this.raw < 25) {
          return `${n} sits inside the range everyone chases, and it still tells you nothing about whether you can carry a pack for ninety minutes. Khepri tracks that instead.`;
        }
        if (this.raw < 30) {
          return `The index says ${n} and cannot tell muscle from anything else. Khepri watches waist, resting heart rate, and whether your lifts still climb while you eat less — those three disagree with BMI often enough to be the ones worth measuring.`;
        }
        return `At ${n} the number is loud, and it is still one weak signal. The first thing Khepri would change is not your weight but how many sessions in a row you finish.`;
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
        if (this.kit.barbell) names.push("a barbell");
        if (this.kit.dumbbells) names.push("dumbbells");
        if (this.kit.pullup) names.push("a pull-up bar");
        return names.length ? names.join(" and ") : "nothing but the floor";
      },

      get summary() {
        const objective = {
          strength: "getting stronger",
          leaner: "getting leaner",
          endurance: "lasting longer",
          pain: "training without pain",
        }[this.goal];
        return `${this.days} ${this.days === 1 ? "day" : "days"} a week, ${objective}, with ${this.owned}. Khepri would spend the first three weeks proving you can hold the schedule before adding any load.`;
      },

      get split() {
        const heavy = this.kit.barbell ? "Barbell" : this.kit.dumbbells ? "Dumbbell" : "Bodyweight";
        const pull = this.kit.pullup ? "pull-ups" : this.kit.dumbbells ? "rows" : "inverted rows";

        const library = {
          strength: [
            { day: "Day 1", work: `${heavy} squat, hinge, and a carry` },
            { day: "Day 2", work: `Press, ${pull}, and a loaded carry` },
            { day: "Day 3", work: `${heavy} hinge and single-leg work` },
            { day: "Day 4", work: "Repeat day 1 at a lighter load" },
            { day: "Day 5", work: "Press variation and arms" },
            { day: "Day 6", work: "Easy walk and mobility" },
          ],
          leaner: [
            { day: "Day 1", work: `${heavy} full body, then twenty minutes easy` },
            { day: "Day 2", work: `${pull}, press, and intervals` },
            { day: "Day 3", work: "Long easy effort, conversational pace" },
            { day: "Day 4", work: `${heavy} full body, heavier and shorter` },
            { day: "Day 5", work: "Intervals and core" },
            { day: "Day 6", work: "Walk, and eat like it is a training day" },
          ],
          endurance: [
            { day: "Day 1", work: "Easy aerobic hour, nose breathing" },
            { day: "Day 2", work: `Strength: ${heavy} squat and ${pull}` },
            { day: "Day 3", work: "Tempo effort, thirty minutes" },
            { day: "Day 4", work: "Long slow distance" },
            { day: "Day 5", work: "Hills or intervals" },
            { day: "Day 6", work: "Recovery walk" },
          ],
          pain: [
            { day: "Day 1", work: "Hinge pattern, unloaded, plus breathing work" },
            { day: "Day 2", work: `Isometrics and ${pull} at low load` },
            { day: "Day 3", work: "Walk, and one tolerance test" },
            { day: "Day 4", work: "Add load to the pattern that stayed quiet" },
            { day: "Day 5", work: "Full body, everything sub-maximal" },
            { day: "Day 6", work: "Easy movement only" },
          ],
        };

        return library[this.goal].slice(0, Number(this.days));
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

    const explicit = t.match(/([1-7])\s*(?:x|times|days?|sessions?)/);
    const days = explicit ? Number(explicit[1]) : 3;

    const kit = [];
    if (/barbell|squat rack|the gym|gym\b/.test(t)) kit.push("barbell");
    if (/dumbbell|db\b/.test(t)) kit.push("dumbbells");
    if (/pull ?-?up|chin ?-?up/.test(t)) kit.push("pull-up bar");
    if (/kettlebell|kb\b/.test(t)) kit.push("kettlebell");
    if (/band/.test(t)) kit.push("bands");
    if (!kit.length) kit.push("bodyweight only");

    const wantsLean = /lose|leaner|lean|fat|weight off|slim|cut/.test(t);
    const wantsStrong = /strong|strength|lift|muscle|bigger|gains/.test(t);
    const wantsEndurance = /run|hike|cycle|endurance|cardio|marathon|fitter/.test(t);

    let objective = "Build a habit that survives a bad week, then add load";
    if (wantsStrong && wantsLean) {
      objective = "Add strength while losing fat — strength leads, the deficit follows it";
    } else if (wantsStrong) {
      objective = "Add strength on the main patterns: squat, hinge, press, pull";
    } else if (wantsLean) {
      objective = "Lose fat without losing the strength you already have";
    } else if (wantsEndurance) {
      objective = "Raise the aerobic base, then add the specific work";
    }

    let constraint = "None mentioned — say so later and the plan changes";
    if (/back/.test(t)) {
      constraint = "Lower back flares up — the hinge is patterned before it is loaded";
    } else if (/knee/.test(t)) {
      constraint = "Knee to work around — depth and volume progress separately";
    } else if (/shoulder/.test(t)) {
      constraint = "Shoulder to work around — pressing angle is chosen before load";
    } else if (/if i'?m lucky|busy|no time|work|kids|travel/.test(t)) {
      constraint = "The week goes wrong often — sessions are built to survive it";
    }

    const heavy = kit.includes("barbell") ? "Barbell" : kit.includes("dumbbells") ? "Dumbbell" : "Bodyweight";
    const pull = kit.includes("pull-up bar") ? "pull-ups" : kit.includes("dumbbells") ? "rows" : "inverted rows";
    const cautious = /back|knee|shoulder/.test(t);

    const sessions = [
      {
        day: "Session 1",
        work: cautious ? `${heavy} squat to a box, ${pull}, and a loaded carry` : `${heavy} squat, ${pull}, and a loaded carry`,
      },
      {
        day: "Session 2",
        work: cautious ? "Hip hinge unloaded, press, and split squats" : `${heavy} hinge, press, and split squats`,
      },
      {
        day: "Session 3",
        work: wantsLean ? "Full body at a lighter load, then twenty easy minutes" : "Full body, one heavy set per pattern",
      },
    ].slice(0, Math.max(2, Math.min(3, days)));

    return {
      objective,
      horizon: "Twelve weeks, reviewed every third week",
      commitment: `${days} ${days === 1 ? "session" : "sessions"} a week, 45 to 60 minutes`,
      equipment: kit.join(", "),
      constraint,
      sessions,
      measure: wantsLean
        ? "In week six, either the squat goes up 5kg or the same weight moves at a lower bodyweight. One of the two, not both."
        : "In week six, the main lift adds 5kg for the same number of clean reps. If it does not, the problem is recovery, not programming.",
    };
  }
})();
