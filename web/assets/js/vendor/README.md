# Vendored JavaScript

Third-party libraries are committed here rather than loaded from a CDN, because the
production binary embeds its assets (`web/assets/assets.go`). The application keeps
working with no third-party origin to trust, block, or outlive us — and a visitor's
browser makes no request we did not put in the repository ourselves.

**Every file here must be self-contained.** No `import` may point at another origin.
A bundle that reaches out to a CDN at runtime defeats the entire reason this directory
exists. Check before committing:

```
grep -oE 'from"[^".][^"]*"' web/assets/js/vendor/<file> | grep -v 'from"\.'
```

## What is here

| File | Package | Version | License | Source |
|---|---|---|---|---|
| `gsap.module.min.js` | gsap | 3.15.0 | [GreenSock standard "no charge"](https://gsap.com/standard-license) | `https://cdn.jsdelivr.net/npm/gsap@3.15.0/+esm` |
| `gsap-scrolltrigger.module.min.js` | gsap (ScrollTrigger) | 3.15.0 | [GreenSock standard "no charge"](https://gsap.com/standard-license) | `https://cdn.jsdelivr.net/npm/gsap@3.15.0/ScrollTrigger.js/+esm` |
| `lenis.module.min.js` | lenis | 1.3.26 | MIT | `https://cdn.jsdelivr.net/npm/lenis@1.3.26/+esm` |
| `three.module.min.js` | three | r169 † | MIT | not recorded |
| `three-gltf-loader.module.js` | three (`examples/jsm/loaders/GLTFLoader.js`) | r169 † | MIT | not recorded |
| `three-meshopt-decoder.module.js` | three (`examples/jsm/libs/meshopt_decoder.module.js`) | r169 † | MIT | not recorded |
| `three-room-environment.module.js` | three (`examples/jsm/environments/RoomEnvironment.js`) | r169 † | MIT | not recorded |
| `alpine.min.js` | alpinejs | 3.15.0 | MIT | not recorded |
| `htmx.min.js` | htmx.org | 2.0.7 | Zero-Clause BSD | not recorded |
| `htmx-ext-sse.js` | htmx-ext-sse | not recorded | Zero-Clause BSD | not recorded |

† The three.js files predate this README and carry no version string. r169 is **inferred**
from a revision constant in the bundle and its `Copyright 2010-2024` header — treat it as
a starting point for an upgrade, not as verified. Confirm against the release notes before
relying on it. The three.js files must all be upgraded together; mixing an `examples/jsm`
module with a different core revision fails in ways that look like scene bugs.

## Adding a file

1. Pin an exact version. Never `@latest`, never a range.
2. Prefer a single-file ESM build. `https://cdn.jsdelivr.net/npm/<pkg>@<version>/+esm`
   bundles a package's ESM entry into one self-contained file.
3. **Check the license header survived.** jsdelivr's `+esm` transform strips comments,
   including `@license` blocks. All three files added in 2026-08 needed their headers
   prepended by hand. Restore the header from the package's own dist build.
4. Name it `<package>.module.min.js`, or `<package>-<submodule>.module.js` for a piece of
   a larger library, matching the three.js entries above.
5. Run the self-contained check at the top of this file.
6. Add a row to the table. A vendored file with no recorded provenance cannot be audited
   or safely upgraded — which is exactly the state the three.js rows are in.

## Known caveat: upgrades and the immutable cache

Application scripts are cache-busted with `?v=` (`utils.ScriptURL`), but the modules in
here are imported by plain absolute path — see `shared/muscle-viewer/viewer.js` and
`landing/scroll.js`. In production `mountAssets` serves `/assets/*` with
`max-age=31536000, immutable`, so **upgrading a file in this directory in place leaves
returning visitors on the old copy for up to a year.**

Until that is fixed properly, an upgrade needs a new filename — bump the version in the
name (`gsap-3.16.module.min.js`) and update the importers. Fixing it properly means
resolving vendor imports through the `?v=` on `import.meta.url`, and it should be done
for every importer at once rather than one module at a time.

## Notes on specific packages

**ScrollTrigger** does not import GSAP. It reads `window.gsap` at registration time, which
is why the ESM bundle has no imports at all. Set `window.gsap = gsap` before calling
`gsap.registerPlugin(ScrollTrigger)`. See `web/assets/js/landing/scroll.js`.

**GSAP core + ScrollTrigger** are free under the GreenSock standard license for the use
North makes of them. That license does have terms — read it before using GSAP in anything
sold as a product with its own end users.
