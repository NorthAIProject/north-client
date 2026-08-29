// Tests for the command palette's matching, ranking and keyboard model.
//
// Run with `task test:js`, which `task test` also runs. Node's built-in
// runner, so this adds no dependency and there is no package.json to maintain.
//
// The palette is the only navigation route to most of the application, and its
// behaviour is the one part of the feature Go cannot reach: the Go tests prove
// the rows and their haystacks are rendered correctly, and stop at the point
// the browser takes over. This picks up there.
//
// It loads the real shipped file rather than a copy of the algorithm. The file
// is an IIFE that registers itself with Alpine, so the stubs below give it just
// enough of a browser to run in, then capture what it handed to Alpine.data.

const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// The tests live in _tests rather than beside the file they cover: web/assets
// embeds the whole js directory into the binary, and go:embed skips names
// beginning with an underscore. Co-locating them would ship this file to every
// browser that loads the application.
const SOURCE = path.join(__dirname, "..", "shared", "command-palette.js");

// Rows as the browser sees them: the attributes web/shared/layout/palette.templ
// renders. Content is deliberately a small fixture rather than the real
// registry — that the haystacks carry the right words is asserted in Go, by
// TestTheHaystackIsLowercasedAndCarriesTheKeywords. What is under test here is
// what the algorithm does with them.
// Food log and Activity timer sit ahead of Calculator on purpose. All three
// match "cal", but only Calculator matches it in its label — so registry order
// and rank order disagree, and a ranking that quietly stopped working would
// change the result rather than leave it alone.
const ROWS = [
  ["Overview", "overview where today stands today dashboard home start"],
  ["Coach", "coach talk it through today chat ai ask assistant conversation"],
  ["Food log", "food log log today see progress body diary eat nutrition calories"],
  ["Activity timer", "activity timer track a session see calories burned body stopwatch session"],
  ["Care", "care water sleep and habits body hydration sleep habits"],
  ["Calculator", "calculator bmr tdee and a macro target body bmr tdee macros calories"],
  ["Meal plans", "meal plans build plans track totals body food diet nutrition"],
  ["Training insights", "training insights volume frequency and adherence progress charts trends"],
];

function makeRow(label, haystack, index) {
  return {
    id: "command-palette-row-" + index,
    dataset: {
      index: String(index),
      label: label.toLowerCase(),
      haystack,
    },
    clicked: 0,
    click() {
      this.clicked++;
    },
  };
}

// palette loads the real file and returns the Alpine component object, with
// $el wired to a stub whose querySelectorAll hands back the fixture rows.
function palette({ apple = true } = {}) {
  const rows = ROWS.map(([label, hay], i) => makeRow(label, hay, i));

  let captured = null;
  const sandbox = {
    navigator: {
      // The file reads userAgentData first, then platform, then userAgent.
      platform: apple ? "MacIntel" : "Win32",
      userAgent: apple ? "Macintosh" : "Windows NT",
    },
    document: { addEventListener() {} },
    window: {
      Alpine: {
        data(_name, definition) {
          captured = definition();
        },
      },
      tui: { dialog: { toggled: 0, toggle() { this.toggled++; } } },
    },
  };
  sandbox.window.navigator = sandbox.navigator;
  sandbox.window.document = sandbox.document;

  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(SOURCE, "utf8"), sandbox);

  assert.ok(captured, "the file did not register a component with Alpine.data");
  captured.$el = { querySelectorAll: () => rows };
  return { component: captured, rows, tui: sandbox.window.tui };
}

// Array.from rebuilds the list in this realm. The component runs inside a vm
// context, so the arrays it returns have that context's Array prototype and
// deepStrictEqual rejects them as not reference-equal despite matching.
function labels(component) {
  return Array.from(component.visible(), (r) => r.dataset.label);
}

// The point of the feature: opening it with nothing typed is a map of the
// product, in the order the registry declares.
test("an empty query shows every page in registry order", () => {
  const { component, rows } = palette();
  assert.strictEqual(component.count, rows.length);
  assert.deepStrictEqual(
    labels(component),
    ROWS.map(([label]) => label.toLowerCase()),
  );
});

// AND across words is what makes a two-word query narrow rather than widen.
test("every word must match, so a two-word query narrows", () => {
  const { component } = palette();
  component.q = "meal plan";
  assert.deepStrictEqual(labels(component), ["meal plans"]);
});

// The case that actually distinguishes AND from OR: "food" alone matches both
// Food log and Meal plans, and only the second word rules one of them out. A
// matcher that accepted either word would return both and quietly widen every
// query the more you typed.
test("a second word excludes rows the first one matched", () => {
  const { component } = palette();

  component.q = "food";
  assert.deepStrictEqual(labels(component).sort(), ["food log", "meal plans"]);

  component.q = "food plan";
  assert.deepStrictEqual(labels(component), ["meal plans"]);
});

test("words may match in any order and across label and keywords", () => {
  const { component } = palette();
  component.q = "train ins";
  assert.deepStrictEqual(labels(component), ["training insights"]);
});

// The ranking that makes the palette predictable: what you are spelling comes
// first, and a page that merely mentions the word follows it.
test("a label prefix outranks a keyword-only match", () => {
  const { component } = palette();
  component.q = "cal";

  const got = labels(component);
  assert.strictEqual(got[0], "calculator", "expected Calculator first, got " + got.join(", "));
  assert.ok(got.includes("food log"), "Food log matches on its 'calories' keyword");
  assert.ok(
    got.indexOf("calculator") < got.indexOf("food log"),
    "a keyword match ranked above a label prefix",
  );
});

// Keywords are the whole reason the matcher can stay a substring test: nothing
// in "Calculator" spells tdee.
test("a keyword finds a page its label does not name", () => {
  const { component } = palette();
  component.q = "tdee";
  assert.deepStrictEqual(labels(component), ["calculator"]);
});

test("a query matching nothing reports no results", () => {
  const { component } = palette();
  component.q = "xyzzy";
  assert.strictEqual(component.count, 0);
  assert.strictEqual(component.activeID, null);
});

test("the result count is announced in words a screen reader can read", () => {
  const { component } = palette();
  component.q = "tdee";
  assert.strictEqual(component.announce, "1 page");
  component.q = "";
  assert.strictEqual(component.announce, ROWS.length + " pages");
});

// The highlight has to start on the best match, or Enter-on-open goes
// somewhere the person did not look at.
test("the first result is active as soon as a query narrows the list", () => {
  const { component } = palette();
  component.q = "cal";
  assert.strictEqual(component.activeID, component.visible()[0].id);
  assert.ok(component.isActive(component.visible()[0]));
});

test("arrow keys move within the visible rows and wrap", () => {
  const { component } = palette();
  const keys = (key) => component.nav({ key, preventDefault() {} });

  keys("ArrowDown");
  assert.strictEqual(component.active, 1);
  keys("ArrowUp");
  assert.strictEqual(component.active, 0);

  // Up from the first row lands on the last, not on nothing.
  keys("ArrowUp");
  assert.strictEqual(component.active, ROWS.length - 1);
  keys("ArrowDown");
  assert.strictEqual(component.active, 0);

  keys("End");
  assert.strictEqual(component.active, ROWS.length - 1);
  keys("Home");
  assert.strictEqual(component.active, 0);
});

// Movement is defined against what is on screen, so the keyboard and the eye
// never disagree about which row is next.
test("arrow keys skip rows the query has filtered out", () => {
  const { component } = palette();
  component.q = "cal";

  const visible = component.visible();
  assert.ok(visible.length > 1, "the fixture needs at least two matches for 'cal'");

  component.nav({ key: "ArrowDown", preventDefault() {} });
  assert.strictEqual(component.activeID, visible[1].id);
});

// A click rather than location.assign, so ⌘-click and middle-click keep
// meaning what the browser says they mean.
test("Enter follows the highlighted row as a real link", () => {
  const { component } = palette();
  component.q = "tdee";
  component.nav({ key: "Enter", preventDefault() {} });

  const calculator = component.visible()[0];
  assert.strictEqual(calculator.dataset.label, "calculator");
  assert.strictEqual(calculator.clicked, 1);
});

test("Enter does nothing when the query matches nothing", () => {
  const { component, rows } = palette();
  component.q = "xyzzy";
  component.nav({ key: "Enter", preventDefault() {} });
  assert.deepStrictEqual(rows.map((r) => r.clicked), rows.map(() => 0));
});

// Typing changes what is visible, so the highlight has to come back to the top
// rather than stay on an index that now means a different row.
test("typing returns the highlight to the best match", () => {
  const { component } = palette();
  component.nav({ key: "ArrowDown", preventDefault() {} });
  component.nav({ key: "ArrowDown", preventDefault() {} });
  assert.strictEqual(component.active, 2);

  component.nav({ key: "c", preventDefault() {} });
  assert.strictEqual(component.active, 0);
});

test("Cmd+K opens the palette on Apple keyboards", () => {
  const { component, tui } = palette({ apple: true });
  let prevented = 0;

  component.hotkey({ key: "k", metaKey: true, preventDefault: () => prevented++ });
  assert.strictEqual(tui.dialog.toggled, 1);
  assert.strictEqual(prevented, 1);
});

// The reason the shortcut is a platform switch and not (meta || ctrl). On
// macOS, Ctrl-K is kill-to-end-of-line inside every text field, including the
// chat composer — claiming it would break a shortcut people use while typing.
test("Ctrl+K is left alone on Apple keyboards", () => {
  const { component, tui } = palette({ apple: true });
  let prevented = 0;

  component.hotkey({ key: "k", ctrlKey: true, preventDefault: () => prevented++ });
  assert.strictEqual(tui.dialog.toggled, 0, "Ctrl+K was claimed on a Mac");
  assert.strictEqual(prevented, 0, "Ctrl+K was prevented on a Mac");
});

test("Ctrl+K opens the palette everywhere else", () => {
  const { component, tui } = palette({ apple: false });

  component.hotkey({ key: "k", ctrlKey: true, preventDefault() {} });
  assert.strictEqual(tui.dialog.toggled, 1);

  // And Cmd is not the binding there, so a stray meta key does nothing.
  component.hotkey({ key: "k", metaKey: true, preventDefault() {} });
  assert.strictEqual(tui.dialog.toggled, 1);
});

test("an unmodified k types instead of opening the palette", () => {
  const { component, tui } = palette();
  component.hotkey({ key: "k", preventDefault() {} });
  assert.strictEqual(tui.dialog.toggled, 0);
});

// Holding the chord must not toggle the dialog once per repeat.
test("a held shortcut opens the palette once", () => {
  const { component, tui } = palette();
  component.hotkey({ key: "k", metaKey: true, preventDefault() {} });
  component.hotkey({ key: "k", metaKey: true, repeat: true, preventDefault() {} });
  assert.strictEqual(tui.dialog.toggled, 1);
});

// An IME composing a character must not be interrupted by the shortcut.
test("a composing keystroke is left to the input method", () => {
  const { component, tui } = palette();
  component.hotkey({ key: "k", metaKey: true, isComposing: true, preventDefault() {} });
  assert.strictEqual(tui.dialog.toggled, 0);
});

// Closing has to clear the query, or reopening shows the last search rather
// than the map of the product.
test("closing resets the query and the highlight", () => {
  const { component } = palette();
  component.q = "tdee";
  component.nav({ key: "ArrowDown", preventDefault() {} });

  component.reset();
  assert.strictEqual(component.q, "");
  assert.strictEqual(component.active, 0);
  assert.strictEqual(component.count, ROWS.length);
});

// The rows carry their registry position, and that is what an unfiltered list
// is ordered by. Ranking must not disturb it.
test("rank falls back to registry order when nothing is typed", () => {
  const { component, rows } = palette();
  assert.deepStrictEqual(
    rows.map((r) => component.rank(r)),
    rows.map((_, i) => i),
  );
});

// Hovering has to agree with the keyboard, since the two share one highlight.
test("hovering a row makes it the active one", () => {
  const { component } = palette();
  const third = component.visible()[2];
  component.activate(third);
  assert.strictEqual(component.activeID, third.id);
});
