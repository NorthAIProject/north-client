/**
 * The two places where the world meets an asset North already ships.
 *
 * Nothing here downloads anything new. The film panel binds to the *same*
 * `<video>` element the page already renders in `heroFilm()`, so the mp4 is
 * decoded once and drawn twice; the mascot is the cutout that
 * `brand/README.md` already earmarked for exactly this.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

const FILM_WIDTH = 5.4;
const FILM_HEIGHT = FILM_WIDTH * (9 / 16);

/**
 * A screen hanging in the world, showing the product film.
 *
 * The texture is bound to the DOM element rather than to a second `<video>` of
 * its own. Two elements would mean two decoders and two downloads of the same
 * 672KB, for two things that are always showing the same frame anyway.
 *
 * @param {THREE.Vector3} position
 * @param {object} palette
 * @param {boolean} reduced   prefers-reduced-motion: show the poster, never the film
 * @param {() => void} onReady called when the poster texture arrives. Under reduced
 *   motion there is no frame loop to pick it up, so the caller has to be told.
 */
export function createFilmPanel(position, palette, reduced, onReady = () => {}) {
  const group = new THREE.Group();
  group.position.copy(position);

  const geometry = new THREE.PlaneGeometry(FILM_WIDTH, FILM_HEIGHT);
  // Starts dark, and goes to white only once there is a texture to tint. A
  // white-based material with no map is a blank glowing rectangle, which is the
  // most conspicuous thing in the scene right up until the poster loads.
  const material = new THREE.MeshBasicMaterial({
    color: 0x11151b,
    transparent: true,
    opacity: 0.92,
    side: THREE.DoubleSide,
  });
  const screen = new THREE.Mesh(geometry, material);
  group.add(screen);

  // The film on the page sits inside a bordered card. Out here the same edge is
  // what stops it reading as a rectangle of light with no object behind it.
  const edges = new THREE.LineSegments(
    new THREE.EdgesGeometry(geometry),
    new THREE.LineBasicMaterial({ color: palette.signal, transparent: true, opacity: 0.5 }),
  );
  group.add(edges);

  const video = document.getElementById("north-hero-film");
  let videoTexture = null;
  let posterTexture = null;
  let disposed = false;

  // The poster is already downloaded — it is the <video>'s own poster attribute —
  // so this is a cache hit, and it is the only thing shown under reduced motion.
  new THREE.TextureLoader().load(
    "/assets/video/north-hero-poster.webp",
    (texture) => {
      if (disposed) { texture.dispose(); return; }
      texture.colorSpace = THREE.SRGBColorSpace;
      posterTexture = texture;
      if (!material.map) {
        material.map = texture;
        material.color.setHex(0xffffff); // stop tinting once there is an image
        material.needsUpdate = true;
      }
      onReady();
    },
    undefined,
    () => {
      // No poster and no video: a plain lit rectangle is still a coherent object
      // in the composition, so there is nothing to recover from here.
    },
  );

  if (video && !reduced) {
    videoTexture = new THREE.VideoTexture(video);
    videoTexture.colorSpace = THREE.SRGBColorSpace;
  }

  return {
    group,

    /**
     * Called when the panel comes into play. Browsers throttle and eventually
     * pause offscreen video, which freezes a VideoTexture on whatever frame it
     * stopped at — so the element is nudged rather than assumed to be running.
     */
    activate() {
      if (!video || reduced) return;
      if (video.paused) video.play().catch(() => {});
    },

    /**
     * Swaps to the live video only once there is a frame to show. Reading from a
     * video with an insufficient readyState paints black, and a black rectangle
     * looks like a bug rather than like a loading state.
     */
    update() {
      if (!videoTexture || !video) return;
      const playable = video.readyState >= video.HAVE_CURRENT_DATA;
      const wanted = playable ? videoTexture : posterTexture;
      if (wanted && material.map !== wanted) {
        material.map = wanted;
        material.color.setHex(0xffffff);
        material.needsUpdate = true;
      }
    },

    dispose() {
      disposed = true;
      geometry.dispose();
      material.dispose();
      edges.geometry.dispose();
      edges.material.dispose();
      // The video element belongs to the page, so the texture is released but the
      // element is left alone — the DOM player keeps working after the world goes.
      if (videoTexture) videoTexture.dispose();
      if (posterTexture) posterTexture.dispose();
    },
  };
}

/**
 * The companion, as a billboard.
 *
 * `brand/README.md` describes north-mascot.png as a "Three.js-ready silhouette"
 * for a living companion, which is this. It is 416KB, so it is loaded on demand
 * — see world.js, which only asks for it once the camera is most of the way to
 * where it stands.
 *
 * @returns {Promise<{sprite: THREE.Sprite, dispose: () => void} | null>}
 */
export function createMascot(position, scale = 3.4) {
  return new Promise((resolve) => {
    new THREE.TextureLoader().load(
      "/assets/brand/north-mascot.png",
      (texture) => {
        texture.colorSpace = THREE.SRGBColorSpace;
        const material = new THREE.SpriteMaterial({
          map: texture,
          transparent: true,
          opacity: 0.85,
          depthWrite: false,
        });
        const sprite = new THREE.Sprite(material);
        sprite.position.copy(position);
        sprite.scale.set(scale, scale, 1);
        resolve({
          sprite,
          dispose() {
            texture.dispose();
            material.dispose();
          },
        });
      },
      undefined,
      // A missing mascot costs the composition one object. Everything else in the
      // world is geometry that cannot fail to load, so there is nothing to retry.
      () => resolve(null),
    );
  });
}
