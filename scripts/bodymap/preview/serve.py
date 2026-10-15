#!/usr/bin/env python3
"""Serves index.html with the repository's web/assets/ mounted at /assets/,
which is where viewer.js imports three.js and the model from. Run from the
repository root: python3 scripts/bodymap/preview/serve.py [port]"""
import http.server
import os
import sys

ROOT = os.getcwd()
HERE = os.path.dirname(os.path.abspath(__file__))


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        path = path.split("?", 1)[0]
        if path.startswith("/assets/"):
            return os.path.join(ROOT, "web", path.lstrip("/"))
        return os.path.join(HERE, path.lstrip("/") or "index.html")

    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def log_message(self, *args):
        pass


port = int(sys.argv[1]) if len(sys.argv) > 1 else 8099
print(f"http://127.0.0.1:{port}/", flush=True)
http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
