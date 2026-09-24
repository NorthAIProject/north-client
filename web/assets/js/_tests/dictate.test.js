// Tests for where dictated words land in a text box.
//
// Run with `task test:js`. The recording itself needs a microphone and a real
// browser; what is under test here is the part that decides what the box holds
// afterwards, because getting that wrong is how a person loses what they had
// already typed.
//
// It loads the real shipped file, like command-palette.test.js does, with just
// enough of a browser stubbed for the IIFE to run and publish its handle.

const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const SOURCE = path.join(__dirname, "..", "shared", "dictate.js");

function load() {
  const window = {
    addEventListener() {},
  };
  const document = {
    readyState: "loading",
    addEventListener() {},
    querySelectorAll() {
      return [];
    },
  };
  vm.runInNewContext(fs.readFileSync(SOURCE, "utf8"), {
    window,
    document,
    navigator: {},
  });
  return window.__northDictate.place;
}

const place = load();

test("an empty box takes the words as they are", () => {
  const out = place("", 0, 0, "slept six hours", NaN);
  assert.strictEqual(out.value, "slept six hours");
  assert.strictEqual(out.cursor, 15);
});

test("words after typed text are separated by a space", () => {
  const out = place("Today I", 7, 7, "ran five kilometres", NaN);
  assert.strictEqual(out.value, "Today I ran five kilometres");
  assert.strictEqual(out.cursor, out.value.length);
});

test("a trailing space already there is not doubled", () => {
  assert.strictEqual(place("Today I ", 8, 8, "ran", NaN).value, "Today I ran");
});

test("words dictated into the middle keep both sides apart", () => {
  const out = place("I felt today", 6, 6, "tired", NaN);
  assert.strictEqual(out.value, "I felt tired today");
  assert.strictEqual(out.cursor, "I felt tired".length);
});

test("a selection is replaced rather than kept", () => {
  assert.strictEqual(place("mood two", 5, 8, "four", NaN).value, "mood four");
});

test("the box's maxlength still holds, and the words are cut rather than lost", () => {
  const out = place("abc", 3, 3, "defghij", 6);
  assert.strictEqual(out.value, "abc de");
});

test("a full box is left alone", () => {
  assert.strictEqual(place("abcdef", 6, 6, "more", 6), null);
});

test("the script installs itself once", () => {
  const window = { addEventListener() {} };
  let listeners = 0;
  const document = {
    readyState: "loading",
    addEventListener() {
      listeners++;
    },
    querySelectorAll() {
      return [];
    },
  };
  const context = { window, document, navigator: {} };
  const source = fs.readFileSync(SOURCE, "utf8");
  vm.runInNewContext(source, context);
  const once = listeners;
  vm.runInNewContext(source, context);
  assert.ok(once > 0);
  assert.strictEqual(listeners, once);
});
