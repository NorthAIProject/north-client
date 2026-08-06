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
//go:embed brand css fonts js models
var Assets embed.FS
