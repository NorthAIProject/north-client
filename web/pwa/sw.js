// North's service worker: an offline shell, and nothing more.
//
// The rule this file is built around has not changed. HTML, POSTs and SSE must
// never be served from a cache: a stale check-in form or a replayed coach
// stream is worse than a failed request. What is cached is only the static
// shell — the stylesheet, the vendored scripts, the fonts and the icons — plus
// one offline page to show when a navigation cannot reach the server.
//
// So the worker can answer "what does North look like" without a network, and
// still cannot answer "what did I say to my coach" from anything but the server.

// Bumping this name is what retires the previous cache. Old caches are deleted
// on activate, so a deploy that changes an asset cannot leave somebody pinned
// to the previous one.
const CACHE = "north-shell-v1";

const OFFLINE_URL = "/offline.html";

// The shell, and only the shell. Deliberately excludes the Three.js, GSAP and
// Lenis bundles used by the marketing landing: they are large, they are lazy
// today, and precaching them would spend a first-run download on pages nobody
// opens offline.
const SHELL = [
  OFFLINE_URL,
  "/assets/css/output.css",
  "/assets/js/vendor/htmx.min.js",
  "/assets/js/vendor/htmx-ext-sse.js",
  "/assets/js/vendor/alpine.min.js",
  "/assets/js/shared/pwa.js",
  "/assets/fonts/geist/geist-variable.woff2",
  "/assets/fonts/geist/geist-mono-variable.woff2",
  "/assets/brand/favicon.svg",
  "/assets/brand/pwa-192.png",
  "/assets/brand/pwa-512.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(CACHE);
      // addAll fails the whole install if any single entry 404s, which would
      // leave the worker permanently uninstalled. Each is added on its own so
      // one renamed asset costs that asset rather than the offline page.
      await Promise.all(
        SHELL.map((url) => cache.add(url).catch(() => undefined)),
      );
      await self.skipWaiting();
    })(),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys();
      await Promise.all(
        names.map((name) => (name === CACHE ? undefined : caches.delete(name))),
      );
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;

  // Anything that changes state goes to the network, always.
  if (request.method !== "GET") {
    return;
  }

  // The coach's stream. Answering this from a cache would replay a reply that
  // was already delivered, which reads as the coach repeating itself.
  const accept = request.headers.get("accept") || "";
  if (accept.includes("text/event-stream")) {
    return;
  }

  // HTMX partials are documents in every way that matters here: they carry
  // CSRF tokens and current state, and a stale one is worse than none.
  if (request.headers.get("HX-Request")) {
    return;
  }

  // Cross-origin requests are somebody else's to cache.
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) {
    return;
  }

  // A page the person navigated to. Always from the network; the offline page
  // stands in only when the network is genuinely unreachable. Note this never
  // caches the real page — a signed-in dashboard in a shared cache is exactly
  // what this worker exists not to do.
  if (request.mode === "navigate") {
    event.respondWith(
      (async () => {
        try {
          return await fetch(request);
        } catch {
          const cached = await caches.match(OFFLINE_URL);
          return (
            cached ||
            new Response("You are offline.", {
              status: 503,
              headers: { "Content-Type": "text/plain; charset=utf-8" },
            })
          );
        }
      })(),
    );
    return;
  }

  // The static shell. Cache-first because these are the files whose content
  // never changes without their URL changing or the cache being retired, and
  // revalidated in the background so a deploy is picked up on the next load
  // rather than the next cache bump.
  if (url.pathname.startsWith("/assets/")) {
    event.respondWith(
      (async () => {
        const cached = await caches.match(request);
        const network = fetch(request)
          .then(async (response) => {
            if (response && response.ok) {
              const cache = await caches.open(CACHE);
              await cache.put(request, response.clone());
            }
            return response;
          })
          .catch(() => undefined);

        // Falling back to the network result when there is nothing cached, and
        // to a plain failure when both are unavailable.
        return (
          cached ||
          (await network) ||
          Response.error()
        );
      })(),
    );
  }

  // Everything else — API reads, anything unrecognised — is left to the
  // browser, which is the safe default.
});
