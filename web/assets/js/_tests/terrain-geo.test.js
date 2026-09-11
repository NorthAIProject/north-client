// Tests for the terrain's arithmetic: polylines, projection and the height
// curve.
//
// Run with `task test:js`, which `task test` also runs. Node's built-in
// runner, so this adds no dependency and there is no package.json to maintain.
//
// It loads the real shipped module rather than a copy of the maths. geo.js is
// an ES module and this file is CommonJS, which is fine: dynamic import()
// works from CJS, so no build step and no change to the test glob. That the
// module imports nothing from three.js is what makes this possible at all —
// there is no renderer to stub and no canvas to fake.

const test = require("node:test");
const assert = require("node:assert");
const path = require("node:path");
const { pathToFileURL } = require("node:url");

const SOURCE = path.join(__dirname, "..", "shared", "strava-activities", "geo.js");
const geo = import(pathToFileURL(SOURCE).href);

test("decodePolyline decodes the canonical fixture", async () => {
  const { decodePolyline } = await geo;

  // Google's own worked example from the polyline algorithm documentation.
  const points = decodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@");

  assert.strictEqual(points.length, 3);
  assert.deepStrictEqual(points[0].map((n) => Number(n.toFixed(5))), [38.5, -120.2]);
  assert.deepStrictEqual(points[1].map((n) => Number(n.toFixed(5))), [40.7, -120.95]);
  assert.deepStrictEqual(points[2].map((n) => Number(n.toFixed(5))), [43.252, -126.453]);
});

test("decodePolyline of an indoor activity is empty, not an error", async () => {
  const { decodePolyline } = await geo;

  assert.deepStrictEqual(decodePolyline(""), []);
  assert.deepStrictEqual(decodePolyline(null), []);
  assert.deepStrictEqual(decodePolyline(undefined), []);
});

// A payload cut short mid-coordinate must stop, not spin. The continuation
// bit says "another chunk follows"; if the string ends first, the original
// loop read NaN forever.
test("decodePolyline terminates on a truncated payload", async () => {
  const { decodePolyline } = await geo;

  const points = decodePolyline("_p~iF~ps|U_ulL");

  assert.ok(Array.isArray(points));
  assert.ok(points.length >= 1, "the complete coordinate should survive");
});

// The cos(latitude) correction is the whole reason this is not a plain
// subtraction: a degree of longitude is half as wide at 60° as at the equator,
// and without the correction every route drawn away from the tropics is
// stretched sideways.
test("projectRoute narrows longitude with latitude", async () => {
  const { projectRoute } = await geo;

  const width = (lat) => {
    const projected = projectRoute([
      [lat, -0.5],
      [lat, 0.5],
    ]);
    return Math.abs(projected[1][0] - projected[0][0]);
  };

  const atEquator = width(0);
  const atSixty = width(60);

  assert.ok(Math.abs(atEquator - 1) < 1e-9, `equator span was ${atEquator}`);
  assert.ok(
    Math.abs(atSixty - 0.5) < 1e-3,
    `a 1° span at 60°N projected to ${atSixty}, want about half the equator's`,
  );
});

test("projectRoute centres a route on itself", async () => {
  const { projectRoute } = await geo;

  const projected = projectRoute([
    [51.5, -0.1],
    [51.6, 0.1],
  ]);

  const meanX = (projected[0][0] + projected[1][0]) / 2;
  const meanY = (projected[0][1] + projected[1][1]) / 2;
  assert.ok(Math.abs(meanX) < 1e-9, `x is centred on ${meanX}`);
  assert.ok(Math.abs(meanY) < 1e-9, `y is centred on ${meanY}`);
});

test("fitToCap preserves aspect ratio", async () => {
  const { fitToCap } = await geo;

  // A route four times as wide as it is tall.
  const fitted = fitToCap(
    [
      [0, 0],
      [4, 0],
      [4, 1],
      [0, 1],
    ],
    2,
  );

  const xs = fitted.map(([x]) => x);
  const ys = fitted.map(([, y]) => y);
  const width = Math.max(...xs) - Math.min(...xs);
  const height = Math.max(...ys) - Math.min(...ys);

  assert.ok(Math.abs(width - 2) < 1e-9, `width ${width}, want the full 2`);
  assert.ok(Math.abs(height - 0.5) < 1e-9, `height ${height}, want 0.5 — a 4:1 route must stay 4:1`);
});

// A treadmill run's polyline is one repeated point, or a single point. A zero
// span divided into the cap size is Infinity, and three.js renders a buffer
// full of NaN as nothing at all while reporting nothing at all.
test("fitToCap of a single point is the origin, not NaN", async () => {
  const { fitToCap } = await geo;

  for (const input of [[[3, 3]], [[3, 3], [3, 3], [3, 3]]]) {
    const fitted = fitToCap(input, 2);
    for (const [x, y] of fitted) {
      assert.ok(Number.isFinite(x) && Number.isFinite(y), `got [${x}, ${y}]`);
      assert.strictEqual(x, 0);
      assert.strictEqual(y, 0);
    }
  }
});

test("loadToHeight floors a rest day at the plate", async () => {
  const { loadToHeight, PLATE_HEIGHT } = await geo;

  assert.strictEqual(loadToHeight(0, 500), PLATE_HEIGHT);
  assert.strictEqual(loadToHeight(-1, 500), PLATE_HEIGHT);
  assert.strictEqual(loadToHeight(300, 0), PLATE_HEIGHT);
});

test("loadToHeight is monotonic and bounded", async () => {
  const { loadToHeight, MAX_HEIGHT, PLATE_HEIGHT } = await geo;

  const scale = 500;
  let previous = PLATE_HEIGHT;
  for (let load = 1; load <= 20000; load += 37) {
    const height = loadToHeight(load, scale);
    assert.ok(height >= previous, `height fell from ${previous} to ${height} at ${load}`);
    assert.ok(height <= MAX_HEIGHT, `height ${height} exceeded the ceiling at ${load}`);
    previous = height;
  }
});

// The point of the soft clip: one enormous day must not tower over an
// ordinary one by the ratio of their loads. A six-hour hike is roughly four
// times a normal session's load, and a linear curve would draw it four times
// as tall, flattening everything else.
test("loadToHeight compresses an outlier rather than letting it tower", async () => {
  const { loadToHeight } = await geo;

  const scale = 500;
  const ordinary = loadToHeight(500, scale);
  const enormous = loadToHeight(2000, scale);

  assert.ok(enormous > ordinary, "a bigger day is still taller");
  assert.ok(
    enormous / ordinary < 2,
    `an outlier four times the load drew ${(enormous / ordinary).toFixed(2)}× the height; the curve is not compressing`,
  );
});

test("clipped marks the days that ran off the top of the scale", async () => {
  const { clipped } = await geo;

  assert.strictEqual(clipped(600, 500), true);
  assert.strictEqual(clipped(400, 500), false);
  assert.strictEqual(clipped(600, 0), false);
});
