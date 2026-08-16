// Network-only. Exists so Chromium treats North as installable and so a
// future cache can land on this URL without changing the registration.
// HTML, POSTs, and SSE must never be cached: a stale check-in form or a
// replayed coach stream is worse than a failed request.
self.addEventListener("install", (event) => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET") {
    return;
  }
  const accept = event.request.headers.get("accept") || "";
  if (accept.includes("text/event-stream")) {
    return;
  }
  event.respondWith(fetch(event.request));
});
