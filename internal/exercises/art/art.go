// Package art holds the exercise pose frames the API embeds: three per
// movement, at <illustration slug>/frame-N.svg.gz, which exercises.API reads
// as path data for the native app.
//
// The frames are stored gzipped, 906 of them in about 11 MB; raw they would
// be 24.7 MB in the binary and in every image layer. NOTICE beside them is the
// licence attribution (CC BY-SA 4.0) and is not embedded.
//
// The browser's copies (plain .svg frames and the loop.gif animations
// Telegram links to) live in north-web-app under static/assets/exercises.
// scripts/workout-guide-art writes these frames; see its doc comment for
// copying them there.
package art

import "embed"

// Frames is the frame tree, rooted at the illustration slugs.
//
//go:embed */frame-*.svg.gz
var Frames embed.FS
