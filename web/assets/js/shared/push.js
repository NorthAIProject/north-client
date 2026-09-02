// Web Push opt-in for nudges.
//
// Hand-rolled rather than an Alpine component, on purpose: the state this
// controls lives in the browser — the permission, the subscription, whether
// this is an installed PWA on iOS — and is read asynchronously from
// navigator.serviceWorker and PushManager. That is not the local UI state
// Alpine is for, and registering an Alpine.data() from a deferred script races
// alpine:init. Plain DOM is shorter and has no ordering to get wrong.
//
// The markup contract is a root with data-push, carrying data-push-key (the
// VAPID public key) and data-csrf, and children with data-push-state="…" that
// are shown for the matching state. A child with data-push-hint gets one line
// of explanation. Buttons: data-push-enable, data-push-disable,
// data-push-dismiss. A root with data-push-card is the dashboard card: it hides
// itself when there is nothing this browser can do, or the person said not now.
(function () {
  "use strict";

  const DISMISSED_KEY = "north.push.dismissed";

  const HINTS = {
    unsupported: "This browser cannot receive notifications.",
    install:
      "Add Khepri to your Home Screen first — Safari only delivers notifications to installed apps.",
    denied:
      "Notifications are blocked for this site. Allow them in your browser settings to turn this on.",
    ready: "A short note when you have not checked in, a goal is due, or it is a training day.",
    subscribed: "This device gets nudges. Quiet hours still apply.",
    working: "One moment…",
    failed: "That did not work. Try again in a moment.",
  };

  function supported() {
    return (
      "serviceWorker" in navigator &&
      "PushManager" in window &&
      "Notification" in window
    );
  }

  // iOS ships Web Push only to PWAs on the Home Screen. Standalone is the
  // signal; a plain Safari tab has to be told to install first.
  function needsInstall() {
    const ios = /iP(hone|ad|od)/.test(navigator.userAgent);
    const standalone =
      window.navigator.standalone === true ||
      window.matchMedia("(display-mode: standalone)").matches;
    return ios && !standalone;
  }

  function dismissed() {
    try {
      return window.localStorage.getItem(DISMISSED_KEY) === "1";
    } catch {
      return false;
    }
  }

  function remember() {
    try {
      window.localStorage.setItem(DISMISSED_KEY, "1");
    } catch {
      // Private mode. The card comes back next visit, which is acceptable.
    }
  }

  // applicationServerKey wants raw bytes; the server hands over base64url.
  function keyBytes(base64url) {
    const padding = "=".repeat((4 - (base64url.length % 4)) % 4);
    const base64 = (base64url + padding).replace(/-/g, "+").replace(/_/g, "/");
    const raw = window.atob(base64);
    const out = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) {
      out[i] = raw.charCodeAt(i);
    }
    return out;
  }

  function init(root) {
    const key = root.dataset.pushKey || "";
    const csrf = root.dataset.csrf || "";
    const isCard = root.hasAttribute("data-push-card");
    const hint = root.querySelector("[data-push-hint]");
    const states = root.querySelectorAll("[data-push-state]");

    function show(state, message) {
      states.forEach((el) => {
        el.hidden = el.dataset.pushState !== state;
      });
      if (hint) {
        hint.textContent = message || HINTS[state] || "";
      }
      // The dashboard card is rendered hidden and only earns its place once
      // this browser is known to be able to deliver.
      if (isCard) {
        root.hidden = !(state === "ready" || state === "working" || state === "failed");
      }
    }

    async function registration() {
      return navigator.serviceWorker.ready;
    }

    async function current() {
      const reg = await registration();
      return reg.pushManager.getSubscription();
    }

    async function send(method, body) {
      const response = await fetch("/app/push/subscriptions", {
        method,
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrf,
        },
        body: JSON.stringify(body),
      });
      if (!response.ok) {
        throw new Error("push subscription request failed: " + response.status);
      }
    }

    async function refresh() {
      if (isCard && dismissed()) {
        root.hidden = true;
        return;
      }
      if (!supported()) {
        show("unsupported");
        return;
      }
      if (needsInstall()) {
        show("install");
        return;
      }
      if (Notification.permission === "denied") {
        show("denied");
        return;
      }
      try {
        const sub = await current();
        show(sub ? "subscribed" : "ready");
      } catch {
        show("unsupported");
      }
    }

    async function enable() {
      show("working");
      try {
        const permission = await Notification.requestPermission();
        if (permission !== "granted") {
          show("denied");
          return;
        }
        const reg = await registration();
        const sub =
          (await reg.pushManager.getSubscription()) ||
          (await reg.pushManager.subscribe({
            userVisibleOnly: true,
            applicationServerKey: keyBytes(key),
          }));
        try {
          await send("POST", sub.toJSON());
        } catch (err) {
          // The server did not keep it, so the browser must not either, or
          // this device would look subscribed to a server that will never
          // send to it.
          await sub.unsubscribe().catch(() => undefined);
          throw err;
        }
        show("subscribed");
        if (isCard) {
          root.hidden = true;
        }
      } catch {
        show("failed");
      }
    }

    async function disable() {
      show("working");
      try {
        const sub = await current();
        if (sub) {
          const endpoint = sub.endpoint;
          await sub.unsubscribe().catch(() => undefined);
          await send("DELETE", { endpoint });
        }
        show("ready");
      } catch {
        show("failed");
      }
    }

    root.querySelectorAll("[data-push-enable]").forEach((el) => {
      el.addEventListener("click", enable);
    });
    root.querySelectorAll("[data-push-disable]").forEach((el) => {
      el.addEventListener("click", disable);
    });
    root.querySelectorAll("[data-push-dismiss]").forEach((el) => {
      el.addEventListener("click", () => {
        remember();
        root.hidden = true;
      });
    });

    refresh();
  }

  function start() {
    document.querySelectorAll("[data-push]").forEach(init);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
