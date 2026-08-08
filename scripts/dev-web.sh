#!/usr/bin/env sh
#
# Builds and runs the web server for the templ watcher.
#
# This exists instead of passing `go run ./cmd/web` to templ's --cmd, because
# `go run` starts the server as a grandchild: templ's child is `go run`, and
# the compiled binary is a child of *that*. When templ restarts the command on
# a file change, or when you Ctrl-C the watcher, it kills `go run` — and the
# server underneath survives, still holding port 8090. A few restarts later you
# have a pile of orphaned servers, a port that is never free, and an app that
# appears not to pick up changes because the old process still owns the socket.
#
# `exec` replaces this shell with the server, so templ's direct child *is* the
# server. Killing it then does what it looks like it does.
#
# A script rather than an inline --cmd because templ does not run --cmd through
# a shell: it splits the string into arguments, so `&&` and `exec` would be
# handed to `go` as literal arguments.
set -eu

go build -o ./tmp/web ./cmd/web
exec ./tmp/web
