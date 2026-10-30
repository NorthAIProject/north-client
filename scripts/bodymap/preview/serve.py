#!/usr/bin/env python3
"""Serves index.html with north-web-app's static/assets/ (the sibling
checkout, or WEB_ASSETS) mounted at /assets/, which is where viewer.js imports
three.js from, and this repository's freshly built scripts/bodymap/body-map.glb
at /assets/models/body-map.glb. Run from the repository root:
python3 scripts/bodymap/preview/serve.py [port]"""
import http.server
import os
import sys

ROOT = os.getcwd()
HERE = os.path.dirname(os.path.abspath(__file__))
WEB_ASSETS = os.environ.get(
    "WEB_ASSETS", os.path.join(ROOT, "..", "north-web-app", "static", "assets")
)
MODEL = os.path.join(ROOT, "scripts", "bodymap", "body-map.glb")


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        path = path.split("?", 1)[0]
        if path == "/assets/models/body-map.glb":
            return MODEL
        if path.startswith("/assets/"):
            return os.path.join(WEB_ASSETS, path[len("/assets/"):])
        return os.path.join(HERE, path.lstrip("/") or "index.html")

    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def log_message(self, *args):
        pass


port = int(sys.argv[1]) if len(sys.argv) > 1 else 8099
print(f"http://127.0.0.1:{port}/", flush=True)
http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
