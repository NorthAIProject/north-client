/**
 * Alpine helper for one pose slot of a progress check (web/progress).
 *
 * Registers `northProgressPhoto()`. On change it redraws the chosen photo
 * onto a canvas at most 1600 px on its long side and puts the JPEG back in the
 * same <input type="file">, so the ordinary multipart form submits it:
 *
 *   - a 12–48 MP phone photo fits under the server's 8 MB cap;
 *   - a canvas export carries no EXIF, so the location a phone wrote into the
 *     original never leaves the device;
 *   - HEIC from a library becomes JPEG, which the server accepts.
 *
 * If any step is unsupported, the original file is kept and the server still
 * checks it. Business rules stay on the server; this only shrinks bytes.
 */
(function () {
  "use strict";

  const MAX_SIDE = 1600;
  const QUALITY = 0.85;

  async function shrink(file) {
    const bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
    const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(bitmap.width * scale);
    canvas.height = Math.round(bitmap.height * scale);
    canvas.getContext("2d").drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", QUALITY));
    if (!blob) throw new Error("toBlob failed");
    const name = file.name.replace(/\.[^.]+$/, "") + ".jpg";
    return new File([blob], name, { type: "image/jpeg" });
  }

  document.addEventListener("alpine:init", () => {
    window.Alpine.data("northProgressPhoto", () => ({
      preview: "",
      busy: false,

      async pick(event) {
        const input = event.target;
        const file = input.files && input.files[0];
        if (this.preview) URL.revokeObjectURL(this.preview);
        this.preview = "";
        if (!file) return;

        this.busy = true;
        try {
          const small = await shrink(file);
          const list = new DataTransfer();
          list.items.add(small);
          input.files = list.files;
          this.preview = URL.createObjectURL(small);
        } catch (_) {
          // Unsupported here: keep the original, the server decides.
          this.preview = URL.createObjectURL(file);
        } finally {
          this.busy = false;
        }
      },

      clear() {
        if (this.preview) URL.revokeObjectURL(this.preview);
        this.preview = "";
        this.$refs.file.value = "";
      },
    }));
  });
})();
