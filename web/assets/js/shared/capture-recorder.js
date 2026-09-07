// Voice notes for quick capture.
//
// Records a clip, posts it to /app/capture/voice, and swaps the reply into the
// capture panel. The transcript comes back in the textarea, not in a preview:
// the person reads what was heard before anything is parsed from it.
//
// Written as delegated listeners on document rather than an Alpine component so
// it survives every htmx swap of the panel with no re-initialisation, and so
// its definition cannot race Alpine's own deferred start.
//
// Tap to start, tap again to stop, rather than press-and-hold. Holding a button
// cannot be done from a keyboard, and the cap here is a full minute — long
// enough that holding a phone still is a worse gesture than pressing twice.
(function () {
	"use strict";

	var PANEL = "capture-panel";
	var MAX_SECONDS = 60;
	var MAX_BYTES = 8 * 1024 * 1024;

	// Speech, mono, at the rate every speech model works in. A minute lands
	// around 1.9 MB, well inside the ceiling, and dropping the second channel
	// and the inaudible top of the spectrum costs a transcript nothing.
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

	var recorder = null;
	var stream = null;
	var chunks = [];
	var timer = null;
	var button = null;

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

	// reveal shows the button only where recording can actually happen. A
	// button that opens a permission prompt and then fails is worse than one
	// that was never offered, and getUserMedia needs a secure context: the
	// installed app has one and http://localhost has one, so this hides itself
	// on plain http and nowhere else that matters.
	function reveal(root) {
		var buttons = (root || document).querySelectorAll("[data-voice-record]");
		for (var i = 0; i < buttons.length; i++) {
			if (supported()) {
				buttons[i].hidden = false;
			}
		}
	}

	// Copy comes from the server, on data- attributes of the record button, so
	// this script holds no English of its own. The fallback is the English
	// anyway: a missing attribute must not blank a label or swallow an error.
	function copy(name, fallback) {
		var button = document.querySelector("[data-voice-record]");
		var value = button && button.getAttribute("data-voice-" + name);
		return value || fallback;
	}

	function setLabel(el, text) {
		var label = el.querySelector("[data-voice-label]");
		if (label) {
			label.textContent = text;
		}
	}

	function idle(el) {
		if (!el) {
			return;
		}
		el.setAttribute("aria-pressed", "false");
		el.removeAttribute("data-recording");
		el.disabled = false;
		setLabel(el, copy("say", "Say it"));
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

	function fail(message) {
		var panel = document.getElementById(PANEL);
		if (!panel) {
			return;
		}
		var box = panel.querySelector("[data-voice-error]");
		if (box) {
			box.textContent = message;
			box.hidden = false;
		}
	}

	function csrf() {
		var field = document.querySelector('input[name="csrf_token"]');
		return field ? field.value : "";
	}

	// toWav re-encodes whatever the browser recorded as 16-bit PCM in a RIFF
	// container.
	//
	// Not a nicety. MediaRecorder produces webm, ogg or mp4 depending on the
	// browser, and the OpenAI dialect every non-Gemini provider speaks names
	// exactly two audio formats: wav and mp3. Uploading the browser's own
	// container would make voice notes work on one provider and fail on the
	// rest, which is the opposite of what the replaceable AI layer is for.
	//
	// The Web Audio API already decodes every container the browser can record,
	// so this costs a dependency of zero.
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

	function send(blob) {
		if (blob.size === 0) {
			idle(button);
			fail(copy("empty", "That recording was empty."));
			return;
		}
		if (blob.size > MAX_BYTES) {
			idle(button);
			fail(copy("toolong", "That recording is too long."));
			return;
		}

		var form = new FormData();
		form.append("audio", blob, "note.wav");

		// The token travels as a header so the CSRF middleware never has to
		// buffer the multipart body to find it.
		fetch("/app/capture/voice", {
			method: "POST",
			body: form,
			headers: { "X-CSRF-Token": csrf(), "HX-Request": "true" },
			credentials: "same-origin",
		})
			.then(function (response) {
				return response.text();
			})
			.then(function (html) {
				var panel = document.getElementById(PANEL);
				if (!panel) {
					return;
				}
				panel.innerHTML = html;
				// The reply carries hx-post attributes of its own; without this
				// the parse button in the swapped-in composer does nothing.
				if (window.htmx) {
					window.htmx.process(panel);
				}
				reveal(panel);
				var box = panel.querySelector("textarea[name='text']");
				if (box) {
					box.focus();
					box.setSelectionRange(box.value.length, box.value.length);
				}
			})
			.catch(function () {
				idle(button);
				fail(copy("unreachable", "That did not reach Khepri. Try again."));
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
		button = el;
		chunks = [];

		navigator.mediaDevices
			.getUserMedia({ audio: true })
			.then(function (granted) {
				stream = granted;

				var type = pickType();
				recorder = type ? new MediaRecorder(granted, { mimeType: type }) : new MediaRecorder(granted);

				recorder.addEventListener("dataavailable", function (event) {
					if (event.data && event.data.size > 0) {
						chunks.push(event.data);
					}
				});

				recorder.addEventListener("stop", function () {
					releaseStream();
					var blob = new Blob(chunks, { type: recorder.mimeType || type || "audio/webm" });
					chunks = [];
					if (button) {
						button.disabled = true;
						setLabel(button, copy("reading", "Reading it\u2026"));
					}
					toWav(blob).then(send, function () {
						idle(button);
						fail(copy("unreadable", "Khepri could not read that recording. Try again."));
					});
				});

				recorder.start();

				el.setAttribute("aria-pressed", "true");
				el.setAttribute("data-recording", "true");
				setLabel(el, copy("stop", "Stop"));

				// The cap is enforced here rather than trusted to the person.
				// A pocket can hold a button for an hour.
				timer = setTimeout(stop, MAX_SECONDS * 1000);
			})
			.catch(function () {
				idle(el);
				fail(copy("mic", "Khepri could not reach the microphone. Check the permission and try again."));
			});
	}

	document.addEventListener("click", function (event) {
		var el = event.target.closest ? event.target.closest("[data-voice-record]") : null;
		if (!el) {
			return;
		}
		event.preventDefault();

		if (!supported()) {
			fail(copy("unsupported", "This browser cannot record audio."));
			return;
		}

		if (recorder && recorder.state === "recording") {
			stop();
			return;
		}
		start(el);
	});

	// Each of the three moments the button can appear: first paint, a panel
	// swap, and a restored back-forward cache page.
	document.addEventListener("DOMContentLoaded", function () {
		reveal(document);
	});
	// htmx 2 dispatched this on the swapped element, so event.target was the new
	// content. htmx 4 dispatches it on the element that made the request and
	// carries the swapped node on the request context instead.
	document.addEventListener("htmx:after:swap", function (event) {
		reveal(event.detail?.ctx?.target || document);
	});
	if (document.readyState !== "loading") {
		reveal(document);
	}
})();
