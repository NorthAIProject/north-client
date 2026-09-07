// PostHog, configured entirely from the server.
//
// Every decision this file could have made — whether to autocapture, whether
// to record, whether the query string is safe to keep — was made in
// web/shared/analytics and arrives as JSON. This script reads it and calls
// posthog. Adding a rule here rather than there puts it somewhere no test can
// reach it.
(function () {
  "use strict";

  var el = document.getElementById("analytics-config");
  if (!el || typeof window.posthog === "undefined") {
    return;
  }

  var cfg;
  try {
    cfg = JSON.parse(el.textContent || "{}");
  } catch (e) {
    return;
  }
  if (!cfg.apiKey || !cfg.host) {
    return;
  }

  // Strips the query string out of anything that looks like a URL property.
  // The server says when a query is safe to keep; this is the enforcement.
  // Applied to the properties PostHog derives from location rather than to
  // the whole object, so a deliberately-sent property is left alone.
  function sanitize(properties) {
    if (cfg.keepQuery) {
      return properties;
    }
    var urlKeys = ["$current_url", "$referrer", "$initial_current_url"];
    for (var i = 0; i < urlKeys.length; i++) {
      var value = properties[urlKeys[i]];
      if (typeof value === "string") {
        var cut = value.search(/[?#]/);
        if (cut >= 0) {
          properties[urlKeys[i]] = value.slice(0, cut);
        }
      }
    }
    return properties;
  }

  window.posthog.init(cfg.apiKey, {
    api_host: cfg.host,

    // Anonymous landing traffic creates no person record. A person appears at
    // identify, which is the moment there is an account to attach one to.
    person_profiles: "identified_only",

    capture_pageview: true,
    autocapture: cfg.autocapture,

    // Replay is started explicitly below when the server allows it, never by
    // configuration — so a page that was not meant to be recorded cannot be
    // recorded by forgetting a flag.
    disable_session_recording: true,
    mask_all_inputs: true,

    // The vendored bundle still carries one loader that would fetch the
    // PostHog toolbar from their CDN. web/assets/js/vendor/README.md explains
    // why nothing in that directory may reach a third-party origin at
    // runtime. This is the switch that holds the line.
    disable_external_dependency_loading: true,

    sanitize_properties: sanitize,
  });

  // The same string the server sends as DistinctId. Without this call the
  // anonymous session and the account are two unrelated people, and no funnel
  // crosses the signup.
  if (cfg.identity) {
    window.posthog.identify(cfg.identity);
  }

  if (cfg.record) {
    window.posthog.startSessionRecording();
  }
})();
