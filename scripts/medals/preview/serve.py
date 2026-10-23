#!/usr/bin/env python3
"""Serves the generated index.html. Run from the repository root: python3 scripts/medals/preview/serve.py [port]"""
import http.server
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        path = path.split("?", 1)[0]
        return os.path.join(HERE, path.lstrip("/") or "index.html")

    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def log_message(self, *args):
        pass


port = int(sys.argv[1]) if len(sys.argv) > 1 else 8098
print(f"http://127.0.0.1:{port}/", flush=True)
http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
