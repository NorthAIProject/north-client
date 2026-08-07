/**
 * The muscle name tables: the mapping between North's 15 muscle keys and the exact
 * mesh names inside body.glb.
 *
 * This lives apart from viewer.js because it has two consumers with nothing else in
 * common. The viewer imports it in the browser to colour meshes; tools/model/build-body.mjs
 * imports it in Node to decide which meshes to keep when it rebuilds body.glb from the
 * source anatomy. The model and the code that reads it are then generated from one
 * list rather than from two that can drift apart.
 *
 * Nothing here may import three.js, or the build script can't load it.
 *
 * internal/workouts/plan/muscle.go carries the same 15 keys for the Go side — see
 * that file's doc comment for the checklist to keep them in sync.
 */

// Exact node names from body.glb, one entry per muscle key. Both sides and every
// anatomical head/segment share a key — that's how a limb pair lights up together.
// The short common-name aliases are a safety net for a future re-export that might
// not match these exact Z-Anatomy labels.
//
// Deep layers are deliberately left out. The atlas models the abdominal wall as four
// stacked sheets and the spinal erectors as eleven, and the viewer draws a muscle as
// a translucent glow that does not write depth — so every extra layer along the same
// view ray adds opacity without adding information, and the torso turns into one flat
// orange slab. Listing only the layers a person could actually point to on their own
// body keeps the highlight legible, and it says the same thing: this exercise works
// your abs. Anything omitted here is also omitted from body.glb, since
// tools/model/build-body.mjs builds the asset from this table.
export const MUSCLE_ALIASES = {
  quads: [
    "rectus femoris muscle",
    "vastus lateralis muscle",
    "vastus medialis muscle",
    "vastus intermedius muscle",
    "quadriceps",
  ],
  glutes: [
    "gluteus medius muscle",
    "gluteus maximus muscle",
    "gluteus minimus muscle",
    "glutes",
  ],
  hamstrings: [
    "long head of biceps femoris",
    "short head of biceps femoris",
    "semimembranosus muscle",
    "semitendinosus muscle",
    "hamstrings",
  ],
  calves: [
    "lateral head of gastrocnemius",
    "medial head of gastrocnemius",
    "soleus muscle",
    "calves",
  ],
  adductors: ["adductor magnus", "adductor longus", "adductor brevis", "adductors"],
  traps: [
    "ascending part of trapezius muscle",
    "descending part of trapezius muscle",
    "transverse part of trapezius muscle",
    "trapezius",
    "traps",
  ],
  delts: [
    "acromial part of deltoid muscle",
    "clavicular part of deltoid muscle",
    "scapular spinal part of deltoid muscle",
    "deltoid",
    "delts",
  ],
  biceps: ["long head of biceps brachii", "short head of biceps brachii", "biceps brachii", "biceps"],
  triceps: [
    "medial head of triceps brachii",
    "lateral head of triceps brachii",
    "long head of triceps brachii",
    "triceps brachii",
    "triceps",
  ],
  forearms: [
    "brachioradialis muscle",
    "flexor carpi radialis",
    "superficial head of pronator teres",
    "deep head of pronator teres",
    "humeral head of flexor carpi ulnaris",
    "ulnar head of flexor carpi ulnaris",
    "pronator quadratus",
    "ulnar head of extensor carpi ulnaris",
    "humeral head of extensor carpi ulnaris",
    "extensor carpi radialis longus",
    "extensor carpi radialis brevis",
    "forearms",
  ],
  lats: ["latissimus dorsi muscle", "latissimus dorsi", "lats"],
  rhomboids: ["rhomboid major muscle", "rhomboid minor muscle", "rhomboids"],
  // Thoracolumbar only. The capitis/colli heads of the same three columns belong to
  // `neck` below — ALIAS_LOOKUP is a Map built in key order, so a mesh listed twice
  // silently belongs to whichever key is written last. Splitting them is also the
  // more accurate read: what a deadlift loads is the thoracolumbar erectors, not the
  // neck extensors.
  erectors: [
    "iliocostalis lumborum muscle",
    "iliocostalis thoracis muscle",
    "longissimus thoracis muscle",
    "spinalis thoracis muscle",
    "semispinalis thoracis muscle",
    "erector spinae",
    "erectors",
  ],
  serratus: ["serratus anterior muscle", "serratus anterior", "serratus"],
  // Surface layers only: the internal oblique and transversus abdominis sit under
  // these two and cover the same area.
  abs: ["rectus abdominis muscle", "external abdominal oblique muscle", "abdominals", "abs"],
  // Deliberately empty: body.glb has no pectoralis mesh, so setLoads finds nothing to
  // colour and skips the key. The group is still canonical — see UnmodelledGroups in
  // internal/workouts/plan/muscle.go, and the note the viewer component renders so a
  // bench press does not appear to train nothing. Fill this in when the model gains
  // pec major/minor.
  chest: [],
  // The cervical heads of the erector columns, kept out of `erectors` above. Not a
  // sternocleidomastoid — body.glb has none — so this highlights neck extension,
  // which is what the neck-trained exercises in the catalog actually are.
  neck: [
    "iliocostalis colli muscle",
    "longissimus capitis muscle",
    "longissimus colli muscle",
    "spinalis capitis muscle",
    "spinalis colli muscle",
    "semispinalis colli muscle",
    "neck",
  ],
};

// Display name + one-line description per muscle key, for the click-to-inspect
// panel. Same 17 keys as MUSCLE_ALIASES and internal/workouts/plan/muscle.go.
// A key whose MUSCLE_ALIASES entry is empty still belongs here: the model cannot
// colour it, but the person clicking around still deserves to be told what it is.
export const MUSCLE_INFO = {
  quads: { name: "Quadriceps", description: "Front of the thigh; straightens the knee and drives out of a squat." },
  glutes: { name: "Glutes", description: "The hip's main extensor — powers standing up from a squat or hinge." },
  hamstrings: { name: "Hamstrings", description: "Back of the thigh; bends the knee and extends the hip in a hinge." },
  calves: { name: "Calves", description: "Back of the lower leg; points the foot, drives a calf raise or a jump." },
  adductors: { name: "Adductors", description: "Inner thigh; pulls the leg toward the midline and stabilises the squat." },
  traps: { name: "Trapezius", description: "Upper back and neck; shrugs the shoulders and stabilises overhead loads." },
  delts: { name: "Deltoids", description: "Cap of the shoulder; raises the arm out to the side or overhead." },
  biceps: { name: "Biceps", description: "Front of the upper arm; bends the elbow, as in a curl or a pull-up." },
  triceps: { name: "Triceps", description: "Back of the upper arm; straightens the elbow, as in a press." },
  forearms: { name: "Forearms", description: "Grip and wrist control; the limiting factor in most pulling work." },
  lats: { name: "Latissimus dorsi", description: "Broadest back muscle; pulls the arm down and in, as in a pull-up or row." },
  rhomboids: { name: "Rhomboids", description: "Between the shoulder blades; pulls them together, as in a row." },
  erectors: { name: "Erector spinae", description: "Runs the length of the spine; keeps the back straight under load." },
  serratus: { name: "Serratus anterior", description: "Side of the ribcage; rotates the shoulder blade during an overhead press." },
  abs: { name: "Abdominals", description: "Front of the core; braces the trunk against load, more than it bends it." },
  chest: { name: "Pectorals", description: "Front of the ribcage; pushes the arm forward and across, as in a bench press." },
  neck: { name: "Neck extensors", description: "Back of the neck; holds the head steady under load and extends it." },
};

// Built once at module scope: normalized alias text -> muscle key. Both the aliases
// here and every incoming mesh name go through the same normalizeName(), so this
// table doesn't need to anticipate GLTFLoader's node-name sanitization itself.
const ALIAS_LOOKUP = new Map();
for (const [key, aliases] of Object.entries(MUSCLE_ALIASES)) {
  for (const alias of aliases) ALIAS_LOOKUP.set(normalizeName(alias), key);
}

// GLTFLoader runs every node name through PropertyBinding.sanitizeNodeName() (so
// glTF names are safe as animation track target paths): spaces become underscores
// and periods are stripped outright, so "Iliocostalis lumborum muscle.001" arrives
// here as "Iliocostalis_lumborum_muscle001" — the duplicate-suffix digits end up
// glued straight onto the word with no separator at all. Unifying separators to
// spaces before stripping trailing digits makes both forms converge on the same
// normalized string regardless of which one a given alias or mesh name started as.
//
// The build script sees the raw glTF names rather than the sanitized ones, which is
// exactly why both go through this same function.
export function normalizeName(name) {
  return (name || "")
    .toLowerCase()
    .replace(/[_.]+/g, " ")
    .replace(/\d+$/, "")
    .replace(/\s+/g, " ")
    .trim();
}

export function resolveKey(name) {
  const normalized = normalizeName(name);
  if (!normalized) return null;
  if (ALIAS_LOOKUP.has(normalized)) return ALIAS_LOOKUP.get(normalized);
  // Fallback: substring containment, for names this exact table doesn't cover.
  for (const [alias, key] of ALIAS_LOOKUP) {
    if (normalized.includes(alias) || alias.includes(normalized)) return key;
  }
  return null;
}
