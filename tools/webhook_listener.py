#!/usr/bin/env python3
"""Simple HTTP server that listens for webhook requests and dumps them to stdout."""

import json
import sys
from datetime import datetime, timezone
from http.server import HTTPServer, BaseHTTPRequestHandler


class WebhookHandler(BaseHTTPRequestHandler):
    def _handle(self):
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length) if content_length > 0 else b""

        print(f"\n{'=' * 60}")
        print(f"  {self.command} {self.path}  [{timestamp}]")
        print(f"{'=' * 60}")

        print("\nHeaders:")
        for key, value in self.headers.items():
            print(f"  {key}: {value}")

        if body:
            print("\nBody:")
            try:
                parsed = json.loads(body)
                print(json.dumps(parsed, indent=2))
            except (json.JSONDecodeError, UnicodeDecodeError):
                print(body.decode("utf-8", errors="replace"))
        else:
            print("\n(no body)")

        print()
        sys.stdout.flush()

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def do_GET(self):
        self._handle()

    def do_POST(self):
        self._handle()

    def do_PUT(self):
        self._handle()

    def do_PATCH(self):
        self._handle()

    def do_DELETE(self):
        self._handle()

    def log_message(self, format, *args):
        pass  # suppress default logging; we print our own


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9999
    server = HTTPServer(("0.0.0.0", port), WebhookHandler)
    print(f"Webhook listener running on http://localhost:{port}")
    print("Press Ctrl+C to stop.\n")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nStopped.")


if __name__ == "__main__":
    main()
