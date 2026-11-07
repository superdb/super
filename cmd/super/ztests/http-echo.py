import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


# A tiny server that echoes the method, Authorization header, and body of each
# request back as a JSON object, used to check that from sends HTTP options.
class handler(BaseHTTPRequestHandler):
    def echo(self):
        if self.path.startswith("/404"):
            self.send_response(404)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        n = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(n).decode() if n else ""
        out = json.dumps({
            "method": self.command,
            "authorization": self.headers.get("Authorization", ""),
            "body": body,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

    do_GET = echo
    do_POST = echo
    do_PUT = echo

    def log_message(self, *args):
        pass


HTTPServer(("127.0.0.1", int(sys.argv[1])), handler).serve_forever()
