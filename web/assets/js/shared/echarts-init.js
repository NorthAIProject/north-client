(function () {
  "use strict";

  var charts = new WeakMap();

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
    chart.setOption(option);
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

  // htmx 4 renamed both events and moved the swapped element: detail.target is
  // gone, and the target now hangs off the request context.
  document.body.addEventListener("htmx:after:swap", function (evt) {
    initAll(evt.detail.ctx.target);
  });
  document.body.addEventListener("htmx:after:settle", function (evt) {
    initAll(evt.detail.ctx.target);
  });
})();
