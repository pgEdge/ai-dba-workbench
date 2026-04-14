#!/usr/bin/env python3
"""Walkthrough helper sidecar API."""

import json
import os
import subprocess
import urllib.request
import urllib.error
from http.server import HTTPServer, BaseHTTPRequestHandler

SECRET_DIR = os.environ.get("SECRET_DIR", "/etc/pgedge/secret")
SERVER_URL = os.environ.get("SERVER_URL", "http://server:8080")
API_KEY_FILE = os.path.join(SECRET_DIR, "anthropic-api-key")
TOKEN_FILE = os.path.join(SECRET_DIR, "helper-token")


def read_token():
    """Read the service token from file."""
    try:
        with open(TOKEN_FILE) as f:
            return f.read().strip()
    except FileNotFoundError:
        return ""


def api_request(method, path, data=None):
    """Make an authenticated request to the workbench API."""
    token = read_token()
    url = f"{SERVER_URL}/api/v1{path}"
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(
        url,
        data=body,
        method=method,
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return {"error": e.code, "message": e.read().decode()}
    except Exception as e:
        return {"error": str(e)}


class Handler(BaseHTTPRequestHandler):
    """Handle walkthrough API requests."""

    def do_GET(self):
        if self.path == "/status":
            self.handle_status()
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path == "/set-api-key":
            self.handle_set_api_key()
        elif self.path == "/add-connection":
            self.handle_add_connection()
        else:
            self.send_error(404)

    def handle_status(self):
        """Check current walkthrough state."""
        key_configured = False
        try:
            with open(API_KEY_FILE) as f:
                key_configured = len(f.read().strip()) > 0
        except FileNotFoundError:
            pass

        # Check if demo connection exists
        conns = api_request("GET", "/connections")
        demo_present = False
        if isinstance(conns, list):
            demo_present = any(
                c.get("name") == "demo-ecommerce"
                for c in conns
            )

        self.respond(200, {
            "api_key_configured": key_configured,
            "demo_data_present": demo_present,
        })

    def handle_set_api_key(self):
        """Write API key and reload server config."""
        body = self.read_body()
        key = body.get("api_key", "").strip()
        if not key:
            self.respond(400, {"error": "api_key required"})
            return

        with open(API_KEY_FILE, "w") as f:
            f.write(key)
        os.chmod(API_KEY_FILE, 0o600)

        # Restart the server container so the AI Overview generator
        # picks up the new API key. SIGHUP reloads config but does
        # not restart subsystems that check credentials at boot.
        subprocess.run(
            ["docker", "restart", "wt-server"],
            capture_output=True,
            timeout=30,
        )

        self.respond(200, {"success": True})

    def handle_add_connection(self):
        """Replace demo with user's real database."""
        body = self.read_body()

        # Find and delete demo connection
        conns = api_request("GET", "/connections")
        if isinstance(conns, list):
            for c in conns:
                if c.get("name") == "demo-ecommerce":
                    api_request(
                        "DELETE",
                        f"/connections/{c['id']}",
                    )
                    break

        # Register new connection
        result = api_request("POST", "/connections", {
            "name": body.get("name", "my-database"),
            "host": body["host"],
            "port": body.get("port", 5432),
            "database_name": body["database_name"],
            "username": body["username"],
            "password": body["password"],
            "ssl_mode": body.get("ssl_mode", "prefer"),
            "is_shared": True,
            "is_monitored": True,
            "description": "Connected via walkthrough",
        })

        # Write API key if provided
        api_key = body.get("api_key", "").strip()
        if api_key:
            with open(API_KEY_FILE, "w") as f:
                f.write(api_key)
            os.chmod(API_KEY_FILE, 0o600)
            # Restart the server container so the AI Overview generator
            # picks up the new API key. SIGHUP reloads config but does
            # not restart subsystems that check credentials at boot.
            subprocess.run(
                ["docker", "restart", "wt-server"],
                capture_output=True,
                timeout=30,
            )

        conn_id = result.get("id", "unknown")
        self.respond(200, {
            "success": True,
            "connection_id": conn_id,
        })

    def read_body(self):
        """Read and parse JSON request body."""
        length = int(self.headers.get("Content-Length", 0))
        if length > 65536:
            self.respond(413, {"error": "Request body too large"})
            return {}
        raw = self.rfile.read(length)
        return json.loads(raw) if raw else {}

    def respond(self, code, data):
        """Send JSON response."""
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):  # noqa: A002
        """Suppress default logging."""
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", 8090))
    server = HTTPServer(("0.0.0.0", port), Handler)
    print(f"Walkthrough helper listening on port {port}")
    server.serve_forever()
