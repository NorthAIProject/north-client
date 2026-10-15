(function () {
  "use strict";

  var charts = new WeakMap();

  // Options name their colours as CSS custom properties, so one option serves
  // both themes and the palette stays in the stylesheet with everything else.
  //
  // ECharts cannot read them. It renders to a canvas, where "var(--border)" is
  // not a colour but an unparseable string, and it fails the way canvas always
  // does: silently, drawing black or nothing. Options in this codebase have
  // carried var() strings for a while and none of them ever resolved.
  //
  // Deliberately not shared/css-color.js: that returns a THREE.Color and pulls
  // three.js in with it, and this is a classic script with no imports. The
  // whole job is one getPropertyValue.
  var CSS_VAR = /^var\(\s*(--[A-Za-z0-9_-]+)\s*\)$/;

  // The tokens are oklch(), which a canvas paints but ECharts' own colour
  // parser does not read: anything it derives (a hover lift, a gradient)
  // comes out black. A 1x1 canvas turns any CSS colour into rgba().
  var probe = null;
  function toRGBA(value) {
    if (!probe) {
      var canvas = document.createElement("canvas");
      canvas.width = canvas.height = 1;
      probe = canvas.getContext("2d", { willReadFrequently: true });
      if (!probe) return value;
    }
    probe.clearRect(0, 0, 1, 1);
    probe.fillStyle = "#000";
    probe.fillStyle = value;
    probe.fillRect(0, 0, 1, 1);
    var d = probe.getImageData(0, 0, 1, 1).data;
    return "rgba(" + d[0] + "," + d[1] + "," + d[2] + "," + +(d[3] / 255).toFixed(3) + ")";
  }

  function resolveColors(node) {
    if (typeof node === "string") {
      var match = node.match(CSS_VAR);
      if (!match) return node;
      var value = getComputedStyle(document.documentElement)
        .getPropertyValue(match[1])
        .trim();
      // An unset property leaves the string alone rather than becoming an
      // empty colour, so a typo is visible instead of invisible.
      return value ? toRGBA(value) : node;
    }
    if (Array.isArray(node)) {
      return node.map(resolveColors);
    }
    if (node && typeof node === "object") {
      var out = {};
      for (var key in node) {
        if (Object.prototype.hasOwnProperty.call(node, key)) {
          out[key] = resolveColors(node[key]);
        }
      }
      return out;
    }
    return node;
  }

  function initChart(el) {
    if (!window.echarts) {
      return;
    }
    var optionId = el.getAttribute("data-echarts-option");
    if (!optionId) {
      return;
    }
    var node = document.getElementById(optionId);
    if (!node || !node.textContent) {
      return;
    }
    var option;
    try {
      option = JSON.parse(node.textContent);
    } catch (_err) {
      return;
    }
    var existing = charts.get(el);
    if (existing) {
      existing.dispose();
      charts.delete(el);
    }
    var chart = window.echarts.init(el, null, { renderer: "canvas" });
    chart.setOption(resolveColors(option));
    charts.set(el, chart);
    el.dataset.echartsReady = "true";
  }

  function initAll(root) {
    var scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll("[data-echarts]").forEach(initChart);
  }

  function onResize() {
    document.querySelectorAll("[data-echarts]").forEach(function (el) {
      var chart = charts.get(el);
      if (chart) {
        chart.resize();
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () {
      initAll(document);
    });
  } else {
    initAll(document);
  }

  window.addEventListener("resize", onResize, { passive: true });

  // Switching theme rewrites the custom properties, and a chart holds the
  // colours it resolved at init. Without this, changing theme leaves every
  // chart painted for the theme it was born in.
  if (window.MutationObserver) {
    new MutationObserver(function () {
      initAll(document);
    }).observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "data-theme"],
    });
  }

  // htmx 4 renamed both events and moved the swapped element: detail.target is
  // gone, and the target now hangs off the request context.
  document.body.addEventListener("htmx:after:swap", function (evt) {
    initAll(evt.detail.ctx.target);
  });
  document.body.addEventListener("htmx:after:settle", function (evt) {
    initAll(evt.detail.ctx.target);
  });
})();
