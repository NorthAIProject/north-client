// Dictation for any text box.
//
// Records a clip, posts it to /app/voice/transcribe, and writes the words into
// the textarea the button names, at the cursor. It never submits anything: the
// person reads what was heard, fixes it, and sends it themselves.
//
// Written as delegated listeners on document rather than an Alpine component so
// it survives every htmx swap with no re-initialisation, and so its definition
// cannot race Alpine's own deferred start.
//
// Tap to start, tap again to stop, rather than press-and-hold. Holding a button
// cannot be done from a keyboard, and the cap is two minutes — long enough that
// holding a phone still is a worse gesture than pressing twice.
(function () {
	"use strict";

	// A page can include the script more than once — a partial swapped in with
	// its own tag — and a second set of listeners would start two recorders on
	// one tap.
	if (window.__northDictate) {
		return;
	}

	var ENDPOINT = "/app/voice/transcribe";

	// Defaults for a button that somehow arrives without its limits. The server
	// hands the real ones over on data- attributes, from internal/voice.
	var MAX_SECONDS = 120;
	var MAX_BYTES = 8 * 1024 * 1024;

	// Speech, mono, at the rate every speech model works in. Two minutes land
	// around 3.8 MB, well inside the ceiling, and dropping the second channel and
	// the inaudible top of the spectrum costs a transcript nothing.
	var RATE = 16000;

	// In preference order. Chrome, Firefox and Android give the first; Safari
	// and iOS give one of the mp4 forms. An empty string lets MediaRecorder
	// pick, which is the last resort rather than the default because the server
	// sniffs the container and only knows five of them.
	var TYPES = [
		"audio/webm;codecs=opus",
		"audio/webm",
		"audio/mp4;codecs=mp4a.40.2",
		"audio/mp4",
		"audio/ogg;codecs=opus",
		"",
	];

	// One recording at a time, across every button on the page. A second
	// microphone would be a second permission, and a second clip whose words
	// have nowhere obvious to go.
	var recorder = null;
	var stream = null;
	var chunks = [];
	var timer = null;

	function supported() {
		return (
			typeof MediaRecorder !== "undefined" &&
			!!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia)
		);
	}

	function pickType() {
		for (var i = 0; i < TYPES.length; i++) {
			if (TYPES[i] === "" || MediaRecorder.isTypeSupported(TYPES[i])) {
				return TYPES[i];
			}
		}
		return "";
	}

	// reveal shows the button only where recording can actually happen.
	// getUserMedia needs a secure context: the installed app has one and
	// http://localhost has one, so this hides itself on plain http and nowhere
	// else that matters.
	function reveal(root) {
		if (!supported()) {
			return;
		}
		var buttons = (root || document).querySelectorAll("[data-dictate]");
		for (var i = 0; i < buttons.length; i++) {
			buttons[i].hidden = false;
		}
	}

	// Copy comes from the server, on data- attributes of the button, so this
	// script holds no English of its own. The fallback is the English anyway: a
	// missing attribute must not blank a label or swallow an error.
	function copy(el, name, fallback) {
		var value = el && el.getAttribute("data-dictate-" + name);
		return value || fallback;
	}

	function limit(el, name, fallback) {
		var value = parseInt(copy(el, name, ""), 10);
		return value > 0 ? value : fallback;
	}

	function setState(el, state) {
		if (!el) {
			return;
		}
		var label = el.querySelector("[data-dictate-label]");
		if (state === "idle") {
			el.removeAttribute("data-state");
			el.setAttribute("aria-pressed", "false");
			el.removeAttribute("aria-busy");
			el.disabled = false;
			if (label) {
				label.textContent = copy(el, "say", "Say it");
			}
			return;
		}
		el.setAttribute("data-state", state);
		if (state === "recording") {
			el.setAttribute("aria-pressed", "true");
			if (label) {
				label.textContent = copy(el, "stop", "Stop");
			}
			return;
		}
		// reading
		el.setAttribute("aria-busy", "true");
		el.disabled = true;
		if (label) {
			label.textContent = copy(el, "reading", "Reading it…");
		}
	}

	function errorBox(el) {
		return el && el.parentElement ? el.parentElement.querySelector("[data-dictate-error]") : null;
	}

	function clearError(el) {
		var box = errorBox(el);
		if (box) {
			box.hidden = true;
			box.textContent = "";
		}
	}

	function fail(el, message) {
		setState(el, "idle");
		var box = errorBox(el);
		if (!box) {
			return;
		}
		box.textContent = message;
		box.hidden = false;
		clearTimeout(box._hide);
		box._hide = setTimeout(function () {
			box.hidden = true;
		}, 6000);
	}

	function releaseStream() {
		if (!stream) {
			return;
		}
		stream.getTracks().forEach(function (track) {
			track.stop();
		});
		stream = null;
	}

	function csrf() {
		var field = document.querySelector('input[name="csrf_token"]');
		return field ? field.value : "";
	}

	function target(el) {
		var id = el.getAttribute("data-dictate-target");
		return id ? document.getElementById(id) : null;
	}

	// place works out the box's new value with the words written in at the
	// selection, rather than replacing what was there: dictating the second half
	// of a sentence you started typing is the common case, not the edge one.
	//
	// Pure, so web/assets/js/_tests can reach it without a browser. Returns null
	// when the box has no room left at all.
	function place(value, start, end, text, max) {
		var before = value.slice(0, start);
		var after = value.slice(end);
		if (before && !/\s$/.test(before)) {
			text = " " + text;
		}
		if (after && !/^\s/.test(after)) {
			text = text + " ";
		}

		// The box's own bound still holds. The words are cut rather than refused:
		// the person has already spoken, and handing back nothing loses words
		// they cannot say again identically.
		if (max > 0) {
			var room = max - before.length - after.length;
			if (room <= 0) {
				return null;
			}
			text = text.slice(0, room);
		}

		return { value: before + text + after, cursor: before.length + text.length };
	}

	function insert(el, text) {
		var box = target(el);
		if (!box) {
			return;
		}

		var value = box.value;
		var start = typeof box.selectionStart === "number" ? box.selectionStart : value.length;
		var end = typeof box.selectionEnd === "number" ? box.selectionEnd : value.length;
		// A box that was never focused reports 0, which would put the words in
		// front of everything already typed.
		if (document.activeElement !== box && start === 0 && end === 0) {
			start = end = value.length;
		}

		var next = place(value, start, end, text, parseInt(box.getAttribute("maxlength"), 10));
		if (!next) {
			return;
		}
		box.value = next.value;
		box.focus();
		box.setSelectionRange(next.cursor, next.cursor);

		// So Alpine bindings, the autosize script and anything else listening
		// see the change the same way they would have seen typing.
		box.dispatchEvent(new Event("input", { bubbles: true }));
	}

	// toWav re-encodes whatever the browser recorded as 16-bit PCM in a RIFF
	// container.
	//
	// The recogniser takes the browser's own containers, but the OpenAI dialect
	// a hosted fallback would speak names exactly two audio formats: wav and
	// mp3. Uploading wav keeps moving between the two a configuration change
	// rather than a client one, and the Web Audio API already decodes every
	// container the browser can record, so this costs a dependency of zero.
	function toWav(blob) {
		var Ctx = window.AudioContext || window.webkitAudioContext;
		var Offline = window.OfflineAudioContext || window.webkitOfflineAudioContext;
		if (!Ctx) {
			return Promise.reject(new Error("no audio context"));
		}

		var ctx = new Ctx();
		return blob
			.arrayBuffer()
			.then(function (bytes) {
				// Safari resolves decodeAudioData through a callback rather than
				// the promise on older versions; the promise form is wrapped so
				// both shapes end up here.
				return new Promise(function (resolve, reject) {
					var decoded = ctx.decodeAudioData(bytes, resolve, reject);
					if (decoded && typeof decoded.then === "function") {
						decoded.then(resolve, reject);
					}
				});
			})
			.then(function (audio) {
				ctx.close();
				if (!Offline) {
					return audio;
				}
				// One channel at RATE: the context does the downmix and the
				// resample, so there is no hand-rolled arithmetic to get wrong.
				var frames = Math.max(1, Math.ceil(audio.duration * RATE));
				var off;
				try {
					off = new Offline(1, frames, RATE);
				} catch (e) {
					// A browser that will not build a context at this rate keeps
					// the source rate. Bigger, still a wav, still accepted.
					return audio;
				}
				var source = off.createBufferSource();
				source.buffer = audio;
				source.connect(off.destination);
				source.start();
				return off.startRendering();
			})
			.then(encodeWav);
	}

	function encodeWav(audio) {
		var samples = audio.getChannelData(0);
		var rate = audio.sampleRate;
		var bytes = new ArrayBuffer(44 + samples.length * 2);
		var view = new DataView(bytes);

		function ascii(offset, text) {
			for (var i = 0; i < text.length; i++) {
				view.setUint8(offset + i, text.charCodeAt(i));
			}
		}

		ascii(0, "RIFF");
		view.setUint32(4, 36 + samples.length * 2, true);
		ascii(8, "WAVE");
		ascii(12, "fmt ");
		view.setUint32(16, 16, true); // PCM header length
		view.setUint16(20, 1, true); // PCM, uncompressed
		view.setUint16(22, 1, true); // mono
		view.setUint32(24, rate, true);
		view.setUint32(28, rate * 2, true); // bytes per second
		view.setUint16(32, 2, true); // bytes per frame
		view.setUint16(34, 16, true); // bits per sample
		ascii(36, "data");
		view.setUint32(40, samples.length * 2, true);

		for (var i = 0; i < samples.length; i++) {
			// Clamped before scaling: a sample outside [-1, 1] would wrap to the
			// opposite extreme and arrive as a click.
			var sample = Math.max(-1, Math.min(1, samples[i]));
			view.setInt16(44 + i * 2, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
		}

		return new Blob([bytes], { type: "audio/wav" });
	}

	// reply reads the endpoint's answer. Every refusal from the handler is JSON
	// carrying words to show; a 429 is the quota panel's HTML, which has no
	// place beside a microphone, so it gets a sentence of its own.
	function reply(el, response) {
		if (response.status === 429) {
			fail(el, copy(el, "limit", "You have dictated a lot for now. Type it instead, or try again later."));
			return;
		}
		return response.json().then(
			function (body) {
				if (response.ok && body && typeof body.text === "string") {
					setState(el, "idle");
					insert(el, body.text);
					return;
				}
				var message = body && body.error && body.error.message;
				fail(el, message || copy(el, "unreadable", "Khepri could not read that recording. Try again."));
			},
			function () {
				fail(el, copy(el, "unreachable", "That did not reach Khepri. Try again."));
			},
		);
	}

	function send(el, blob) {
		if (blob.size === 0) {
			fail(el, copy(el, "empty", "That recording was empty."));
			return;
		}
		if (blob.size > limit(el, "max-bytes", MAX_BYTES)) {
			fail(el, copy(el, "toolong", "That recording is too long."));
			return;
		}

		var form = new FormData();
		form.append("audio", blob, "note.wav");
		var surface = el.getAttribute("data-dictate-surface");
		if (surface) {
			form.append("surface", surface);
		}

		// The token travels as a header so the CSRF middleware never has to
		// buffer the multipart body to find it.
		fetch(ENDPOINT, {
			method: "POST",
			body: form,
			headers: { "X-CSRF-Token": csrf() },
			credentials: "same-origin",
		})
			.then(function (response) {
				return reply(el, response);
			})
			.catch(function () {
				fail(el, copy(el, "unreachable", "That did not reach Khepri. Try again."));
			});
	}

	function stop() {
		if (timer) {
			clearTimeout(timer);
			timer = null;
		}
		if (recorder && recorder.state !== "inactive") {
			recorder.stop();
		}
	}

	function start(el) {
		chunks = [];
		clearError(el);

		navigator.mediaDevices
			.getUserMedia({ audio: true })
			.then(function (granted) {
				stream = granted;

				var type = pickType();
				var current = type ? new MediaRecorder(granted, { mimeType: type }) : new MediaRecorder(granted);
				recorder = current;

				current.addEventListener("dataavailable", function (event) {
					if (event.data && event.data.size > 0) {
						chunks.push(event.data);
					}
				});

				current.addEventListener("stop", function () {
					releaseStream();
					var blob = new Blob(chunks, { type: current.mimeType || type || "audio/webm" });
					chunks = [];
					recorder = null;
					setState(el, "reading");
					toWav(blob).then(
						function (wav) {
							send(el, wav);
						},
						function () {
							fail(el, copy(el, "unreadable", "Khepri could not read that recording. Try again."));
						},
					);
				});

				current.start();
				setState(el, "recording");

				// The cap is enforced here rather than trusted to the person.
				// A pocket can hold a button for an hour.
				timer = setTimeout(stop, limit(el, "max-seconds", MAX_SECONDS) * 1000);
			})
			.catch(function () {
				fail(el, copy(el, "mic", "Khepri could not reach the microphone. Check the permission and try again."));
			});
	}

	// Doubles as the guard above and as the tests' handle on the arithmetic.
	window.__northDictate = { place: place };

	document.addEventListener("click", function (event) {
		var el = event.target.closest ? event.target.closest("[data-dictate]") : null;
		if (!el) {
			return;
		}
		event.preventDefault();

		if (!supported()) {
			fail(el, copy(el, "unsupported", "This browser cannot record audio."));
			return;
		}

		if (recorder && recorder.state === "recording") {
			// Any button stops the recording in progress; only the one that
			// started it receives the words.
			stop();
			return;
		}
		if (el.getAttribute("data-state") === "reading") {
			return;
		}
		start(el);
	});

	// Each of the three moments a button can appear: first paint, an htmx swap,
	// and a restored back-forward cache page.
	document.addEventListener("DOMContentLoaded", function () {
		reveal(document);
	});
	// The whole document rather than the swapped node: htmx 4 dispatches this on
	// the element that made the request, which is often not where the new
	// buttons are, and a query over one page costs nothing worth saving.
	document.addEventListener("htmx:after:swap", function () {
		reveal(document);
	});
	window.addEventListener("pageshow", function () {
		reveal(document);
	});
	if (document.readyState !== "loading") {
		reveal(document);
	}
})();
