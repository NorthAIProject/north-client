package assets

import "embed"

// Assets is the static file tree baked into the production binary.
//
// The directories are named without a glob on purpose. "js/*" expands to each
// entry inside js/, and an entry that is an empty directory makes the build
// fail — so simply creating a folder before putting anything in it breaks the
// build in a way that has nothing to do with the change being made. Naming the
// directory embeds it recursively and skips empty subtrees quietly.
//
// exercises is 906 gzipped SVG frames, about 11 MB. They are stored .svg.gz
// rather than .svg because nothing here mounts compression middleware: see
// mountAssets in cmd/web/main.go, which serves those bytes verbatim under
// Content-Encoding: gzip. Raw they would be 24.7 MB, on disk and on the wire.
//
//go:embed brand css exercises fonts js models video
var Assets embed.FS
