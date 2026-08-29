// Command palette behaviour.
//
// The rows are already on the page, rendered from web/shared/layout/destinations.go.
// Nothing here fetches, and nothing here holds a second copy of the registry:
// filtering reads data-label and data-haystack off the anchors themselves.
//
// Ranking is applied as CSS `order` on a flex column, so a keystroke assigns
// about thirty integers and never rebuilds the list.
(function () {
  "use strict";

  // Apple keyboards are the reason this is a switch rather than
  // (metaKey || ctrlKey). On macOS, Ctrl-K is the emacs kill-to-end-of-line
  // binding inside every native text field, including the chat composer.
  // Claiming it there would break a shortcut people use while typing.
  const APPLE = /Mac|iPhone|iPad|iPod/.test(
    (navigator.userAgentData && navigator.userAgentData.platform) ||
      navigator.platform ||
      navigator.userAgent,
  );

  // Rank buckets, best first. Deliberately coarse: over thirty-one items a
  // finer scale would be tuning for its own sake, and a person can predict
  // "the thing my query starts spelling comes first".
  const RANK_LABEL_PREFIX = 0;
  const RANK_LABEL_WORD = 1;
  const RANK_LABEL_SUBSTRING = 2;
  const RANK_LABEL_TOKENS = 3;
  const RANK_KEYWORDS = 4;

  // Multiplier keeps registry order as the tie-break inside a bucket, so the
  // list never reshuffles arbitrarily between two equally good matches.
  const BUCKET = 1000;

  function definition() {
    // A plain closure, deliberately outside the Alpine data object so it is
    // not reactive: every row binds x-show, :style and :data-active, so a
    // single keystroke asks "what is visible?" upwards of thirty times.
    // Recomputing the filter and sort each time would be wasteful, and
    // caching it in reactive state would mean writing during a read.
    //
    // Reading this.q inside the getters below is what registers the
    // dependency, so Alpine still re-evaluates when the query changes; the
    // cache only stops the work being repeated within one evaluation pass.
    const cache = { q: null, rows: null, visible: null };

    return {
      q: "",
      active: 0,
      hotkeyLabel: APPLE ? "⌘K" : "Ctrl K",

      // The rows never change: the palette is server-rendered once per page.
      rows() {
        if (!cache.rows) {
          cache.rows = Array.from(this.$el.querySelectorAll("[data-palette-row]"));
        }
        return cache.rows;
      },

      tokens() {
        const q = this.q.trim().toLowerCase();
        return q ? q.split(/\s+/) : [];
      },

      // A row matches when every token appears somewhere in its haystack.
      // AND across words is what makes "meal plan" find Meal plans and
      // "train ins" find Training insights.
      matches(el) {
        const tokens = this.tokens();
        if (tokens.length === 0) return true;
        const hay = el.dataset.haystack || "";
        return tokens.every((t) => hay.includes(t));
      },

      rank(el) {
        const index = parseInt(el.dataset.index, 10) || 0;
        const q = this.q.trim().toLowerCase();
        if (!q) return index;

        const label = el.dataset.label || "";
        let bucket = RANK_KEYWORDS;

        if (label.startsWith(q)) {
          bucket = RANK_LABEL_PREFIX;
        } else if (label.split(/\s+/).some((w) => w.startsWith(q))) {
          bucket = RANK_LABEL_WORD;
        } else if (label.includes(q)) {
          bucket = RANK_LABEL_SUBSTRING;
        } else if (this.tokens().every((t) => label.includes(t))) {
          bucket = RANK_LABEL_TOKENS;
        }

        return bucket * BUCKET + index;
      },

      // Visible rows in the order they are displayed. Everything about
      // arrow-key movement and Enter is defined against this list, so the
      // keyboard always agrees with what is on screen.
      visible() {
        const q = this.q;
        if (cache.q !== q || !cache.visible) {
          cache.q = q;
          cache.visible = this.rows()
            .filter((el) => this.matches(el))
            .sort((a, b) => this.rank(a) - this.rank(b));
        }
        return cache.visible;
      },

      get count() {
        return this.visible().length;
      },

      get announce() {
        const n = this.count;
        return n === 1 ? "1 page" : n + " pages";
      },

      get activeID() {
        const rows = this.visible();
        const row = rows[Math.min(this.active, rows.length - 1)];
        return row ? row.id : null;
      },

      isActive(el) {
        return el.id === this.activeID;
      },

      activate(el) {
        const at = this.visible().indexOf(el);
        if (at >= 0) this.active = at;
      },

      hotkey(event) {
        const hit = APPLE
          ? event.metaKey && !event.ctrlKey
          : event.ctrlKey && !event.metaKey;
        if (!hit || event.key.toLowerCase() !== "k") return;
        if (event.repeat || event.isComposing) return;

        event.preventDefault();
        if (window.tui && window.tui.dialog) {
          window.tui.dialog.toggle("command-palette");
        }
      },

      nav(event) {
        const rows = this.visible();

        switch (event.key) {
          case "ArrowDown":
            event.preventDefault();
            if (rows.length) this.active = (this.active + 1) % rows.length;
            break;
          case "ArrowUp":
            event.preventDefault();
            if (rows.length) {
              this.active = (this.active - 1 + rows.length) % rows.length;
            }
            break;
          case "Home":
            event.preventDefault();
            this.active = 0;
            break;
          case "End":
            event.preventDefault();
            this.active = Math.max(rows.length - 1, 0);
            break;
          case "Enter": {
            const row = rows[Math.min(this.active, rows.length - 1)];
            if (!row) return;
            event.preventDefault();
            // A click rather than location.assign, so the browser decides what
            // following this link means.
            row.click();
            break;
          }
          default:
            // Any other key edits the query, which can change what is visible.
            // Resetting keeps the highlight on the best match instead of
            // stranding it on whatever index it happened to hold.
            this.active = 0;
        }
      },

      reset() {
        this.q = "";
        this.active = 0;
      },
    };
  }

  // Alpine is deferred in the document head and this file is deferred too, so
  // whether alpine:init has already fired depends on document order. Handle
  // both rather than depending on it.
  const register = () => window.Alpine.data("commandPalette", definition);
  if (window.Alpine) {
    register();
  } else {
    document.addEventListener("alpine:init", register);
  }
})();
