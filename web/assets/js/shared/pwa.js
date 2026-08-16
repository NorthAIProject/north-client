if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // Registration failing (private mode, http on a phone) must not
      // break the page. Installability just stays unavailable.
    });
  });
}
