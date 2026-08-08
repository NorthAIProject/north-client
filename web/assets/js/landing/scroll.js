/**
 * Scroll driver for the marketing page.
 *
 * One place decides how far down the page we are. The bearing rail reads it and
 * so does the 3D world, which is the point: a rail that says 60% while the
 * camera is somewhere else reads as a bug even though neither number is wrong.
 *
 * Imported lazily by landing.js, so a visitor who never scrolls past the hero
 * never pays for GSAP or Lenis.
 */

import gsap from "/assets/js/vendor/gsap.module.min.js";
import ScrollTrigger from "/assets/js/vendor/gsap-scrolltrigger.module.min.js";
import Lenis from "/assets/js/vendor/lenis.module.min.js";

// ScrollTrigger has no import of its own: it picks the core up off `window.gsap`
// when it registers. Assigning first is not optional — see vendor/README.md.
window.gsap = gsap;
gsap.registerPlugin(ScrollTrigger);

/**
 * Starts the driver.
 *
 * @param {object} options
 * @param {boolean} options.reduced  prefers-reduced-motion is set.
 * @returns {{
 *   progress: number,
 *   sections: Array<{el: Element, start: number, end: number, self: number}>,
 *   muscle: {el: Element, self: number, active: boolean} | null,
 *   onProgress: (fn: (p: number) => void) => void,
 *   refresh: () => void,
 *   destroy: () => void,
 * }}
 */
export function createScrollDriver({ reduced = false } = {}) {
  const listeners = new Set();
  const triggers = [];

  // Every listener is handed the current progress. Anything that changes what a
  // listener would do with that number has to call this, not only the thing that
  // moves the number — otherwise the change waits for the next scroll tick and,
  // if the visitor has stopped scrolling, waits indefinitely.
  const publish = () => listeners.forEach((fn) => fn(state.progress));

  const state = {
    progress: 0,
    sections: [],
    muscle: null,
    onProgress(fn) {
      listeners.add(fn);
      fn(state.progress);
      return () => listeners.delete(fn);
    },
    refresh() {
      ScrollTrigger.refresh();
    },
    destroy() {
      triggers.forEach((t) => t.kill());
      triggers.length = 0;
      listeners.clear();
      if (lenis) {
        gsap.ticker.remove(tick);
        lenis.destroy();
        lenis = null;
      }
      document.removeEventListener("click", onAnchorClick);
    },
  };

  // ---------------------------------------------------------------------------
  // Smooth scrolling
  // ---------------------------------------------------------------------------

  let lenis = null;
  const tick = (time) => lenis.raf(time * 1000); // gsap.ticker is seconds, Lenis wants ms

  if (!reduced) {
    lenis = new Lenis({
      // Touch is left alone. Smoothing a finger already touching the surface it
      // is moving is the version of this that people describe as broken, and
      // mobile browsers already do their own momentum.
      syncTouch: false,
      smoothWheel: true,
    });

    // One clock. Left on separate rAF loops, Lenis and ScrollTrigger disagree by
    // a frame and everything scrubbed off scroll position develops a shimmer.
    lenis.on("scroll", ScrollTrigger.update);
    gsap.ticker.add(tick);
    gsap.ticker.lagSmoothing(0);
  }

  // ---------------------------------------------------------------------------
  // In-page anchors
  // ---------------------------------------------------------------------------

  /**
   * Lenis does not intercept anchors, and the browser's own jump fights the
   * smoothing — the page lands, then slides somewhere else. The hero's
   * "See it work" button (href="#product-film") is the one that matters today.
   */
  function onAnchorClick(event) {
    if (!lenis || event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;

    const link = event.target.closest('a[href^="#"]');
    if (!link) return;

    const id = link.getAttribute("href").slice(1);
    if (!id) return;

    const target = document.getElementById(id);
    if (!target) return;

    event.preventDefault();
    lenis.scrollTo(target, { offset: -80 }); // clears the fixed navbar
    // The URL should still change, or the back button loses a step the visitor
    // thinks they took.
    history.pushState(null, "", `#${id}`);
  }

  document.addEventListener("click", onAnchorClick);

  // ---------------------------------------------------------------------------
  // Progress
  // ---------------------------------------------------------------------------

  // Measured against the scroll range itself rather than against a trigger
  // element. `trigger: documentElement` with "bottom bottom" is the obvious
  // spelling and it is a trap: it resolves to the height of <html>, which any
  // stylesheet can pin to one viewport, and the whole page then reports 0%
  // forever with nothing visibly broken to point at.
  triggers.push(
    ScrollTrigger.create({
      start: 0,
      end: () => ScrollTrigger.maxScroll(window),
      onUpdate: (self) => {
        state.progress = self.progress;
        publish();
      },
    }),
  );

  // Every <section> on the page becomes a waypoint, found by structure rather
  // than by a list of names. Adding a section to the page adds a waypoint to the
  // world, which is the behaviour someone editing the copy would expect.
  document.querySelectorAll("main section").forEach((el) => {
    const entry = { el, start: 0, end: 0, self: 0 };
    state.sections.push(entry);
    triggers.push(
      ScrollTrigger.create({
        trigger: el,
        start: "top bottom",
        end: "bottom top",
        onUpdate: (self) => {
          entry.self = self.progress;
        },
        onRefresh: (self) => {
          const max = ScrollTrigger.maxScroll(window) || 1;
          entry.start = self.start / max;
          entry.end = self.end / max;
        },
      }),
    );
  });

  // The muscle demo runs its own WebGL context. The world needs to know when it
  // is on screen so it can get out of the way — visually and on the GPU.
  const muscleEl = document.querySelector('[x-data="northMuscle"]');
  if (muscleEl) {
    const muscle = { el: muscleEl, self: 0, active: false };
    state.muscle = muscle;
    triggers.push(
      ScrollTrigger.create({
        trigger: muscleEl,
        start: "top bottom",
        end: "bottom top",
        onUpdate: (self) => {
          muscle.self = self.progress;
        },
        onToggle: (self) => {
          muscle.active = self.isActive;
          // ScrollTrigger runs onToggle after the progress callbacks in the same
          // update, so the world would otherwise not learn about this until the
          // next one — leaving it at full brightness over the muscle demo for
          // anyone who stops scrolling exactly there.
          publish();
        },
      }),
    );
  }

  return state;
}
