#!/usr/bin/env python3
"""TLS upstream that verifies client certificates and closes every connection."""

import hashlib
import http.server
import json
import os
import ssl


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps({
            "client_fingerprint": hashlib.sha256(
                self.connection.getpeercert(binary_form=True)
            ).hexdigest(),
            "server_name": self.connection.requested_server_name,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(body)
        self.close_connection = True

    def log_message(self, _format, *_args):
        pass


def serve():
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(os.environ.get("TLS_CERT", "/tls/tls.crt"),
                            os.environ.get("TLS_KEY", "/tls/tls.key"))
    context.load_verify_locations(os.environ.get("CLIENT_CA", "/client-ca/ca.crt"))
    context.verify_mode = ssl.CERT_REQUIRED
    context.set_servername_callback(
        lambda connection, name, _context: setattr(connection, "requested_server_name", name)
    )
    server = http.server.ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PORT", "8443"))), Handler)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


if __name__ == "__main__":
    serve()
