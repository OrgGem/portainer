"""Example application: validate runtime injection without disclosing the secret."""
import os
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        ready = bool(os.environ.get("DB_PASSWORD"))
        self.send_response(200 if ready else 503)
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(b"ready\n" if ready else b"secret unavailable\n")


HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
