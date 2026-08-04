/**
 * North passkey (WebAuthn) client for login and signup pages.
 *
 * Expects:
 *  - a button#passkey-btn with data-passkey-mode="login"|"register"
 *  - a hidden csrf_token input (from the password form)
 *  - optional #email, #display_name, #timezone fields on signup
 *  - optional #passkey-error for status messages
 */
(function () {
  "use strict";

  function csrfToken() {
    var el = document.querySelector('input[name="csrf_token"]');
    return el ? el.value : "";
  }

  function showError(msg) {
    var el = document.getElementById("passkey-error");
    if (!el) {
      alert(msg);
      return;
    }
    el.textContent = msg;
    el.classList.remove("hidden");
  }

  function clearError() {
    var el = document.getElementById("passkey-error");
    if (el) {
      el.textContent = "";
      el.classList.add("hidden");
    }
  }

  function bufferToBase64url(buf) {
    var bytes = new Uint8Array(buf);
    var str = "";
    for (var i = 0; i < bytes.length; i++) {
      str += String.fromCharCode(bytes[i]);
    }
    return btoa(str).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function base64urlToBuffer(base64url) {
    var pad = "=".repeat((4 - (base64url.length % 4)) % 4);
    var base64 = (base64url + pad).replace(/-/g, "+").replace(/_/g, "/");
    var str = atob(base64);
    var bytes = new Uint8Array(str.length);
    for (var i = 0; i < str.length; i++) {
      bytes[i] = str.charCodeAt(i);
    }
    return bytes.buffer;
  }

  function preparePublicKeyOptions(publicKey) {
    var opts = Object.assign({}, publicKey);
    opts.challenge = base64urlToBuffer(publicKey.challenge);
    if (publicKey.user && publicKey.user.id) {
      opts.user = Object.assign({}, publicKey.user, {
        id: base64urlToBuffer(publicKey.user.id),
      });
    }
    if (publicKey.excludeCredentials) {
      opts.excludeCredentials = publicKey.excludeCredentials.map(function (c) {
        return Object.assign({}, c, { id: base64urlToBuffer(c.id) });
      });
    }
    if (publicKey.allowCredentials) {
      opts.allowCredentials = publicKey.allowCredentials.map(function (c) {
        return Object.assign({}, c, { id: base64urlToBuffer(c.id) });
      });
    }
    return opts;
  }

  function credentialToJSON(cred) {
    if (!cred) return null;
    var response = cred.response;
    var out = {
      id: cred.id,
      rawId: bufferToBase64url(cred.rawId),
      type: cred.type,
      response: {},
      clientExtensionResults: cred.getClientExtensionResults
        ? cred.getClientExtensionResults()
        : {},
    };
    if (response.attestationObject) {
      out.response = {
        clientDataJSON: bufferToBase64url(response.clientDataJSON),
        attestationObject: bufferToBase64url(response.attestationObject),
        transports:
          typeof response.getTransports === "function"
            ? response.getTransports()
            : undefined,
      };
    } else {
      out.response = {
        clientDataJSON: bufferToBase64url(response.clientDataJSON),
        authenticatorData: bufferToBase64url(response.authenticatorData),
        signature: bufferToBase64url(response.signature),
        userHandle: response.userHandle
          ? bufferToBase64url(response.userHandle)
          : null,
      };
    }
    return out;
  }

  async function api(path, body) {
    var res = await fetch(path, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken(),
      },
      credentials: "same-origin",
      body: JSON.stringify(body || {}),
    });
    var data = {};
    try {
      data = await res.json();
    } catch (_) {
      /* empty */
    }
    if (!res.ok) {
      var err = new Error(data.error || "Request failed");
      err.fields = data.fields;
      err.status = res.status;
      throw err;
    }
    return data;
  }

  async function registerWithPasskey() {
    var email = (document.getElementById("email") || {}).value || "";
    var displayName =
      (document.getElementById("display_name") || {}).value || "";
    var timezone = (document.getElementById("timezone") || {}).value || "";

    if (!email.trim() || !displayName.trim()) {
      showError("Enter your name and email before creating a passkey.");
      return;
    }

    var begin = await api("/auth/passkey/register/begin", {
      email: email.trim(),
      display_name: displayName.trim(),
      timezone: timezone,
    });

    var publicKey = preparePublicKeyOptions(begin.publicKey);
    var cred = await navigator.credentials.create({ publicKey: publicKey });
    var finish = await api("/auth/passkey/register/finish", {
      challenge_id: begin.challenge_id,
      credential: credentialToJSON(cred),
    });
    window.location.href = finish.redirect || "/app";
  }

  async function loginWithPasskey() {
    var email = (document.getElementById("email") || {}).value || "";
    var nextInput = document.querySelector('input[name="next"]');
    var next = nextInput ? nextInput.value : "";
    var finishPath = "/auth/passkey/login/finish";
    if (next) {
      finishPath += "?next=" + encodeURIComponent(next);
    }

    var begin = await api("/auth/passkey/login/begin", {
      email: email.trim(),
    });

    var publicKey = preparePublicKeyOptions(begin.publicKey);
    var cred = await navigator.credentials.get({ publicKey: publicKey });
    var finish = await api(finishPath, {
      challenge_id: begin.challenge_id,
      credential: credentialToJSON(cred),
    });
    window.location.href = finish.redirect || "/app";
  }

  function onReady() {
    var btn = document.getElementById("passkey-btn");
    if (!btn || !window.PublicKeyCredential) {
      if (btn) {
        btn.disabled = true;
        btn.title = "Passkeys are not supported in this browser.";
      }
      return;
    }

    btn.addEventListener("click", function () {
      clearError();
      var mode = btn.getAttribute("data-passkey-mode") || "login";
      var run = mode === "register" ? registerWithPasskey : loginWithPasskey;
      btn.disabled = true;
      run()
        .catch(function (err) {
          if (err && err.name === "NotAllowedError") {
            showError("Passkey was cancelled.");
            return;
          }
          showError((err && err.message) || "Passkey failed. Please try again.");
        })
        .finally(function () {
          btn.disabled = false;
        });
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", onReady);
  } else {
    onReady();
  }
})();
