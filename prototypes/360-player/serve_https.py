#!/usr/bin/env python3
"""HTTPS static server for the 360 prototype (gyro/WebXR require secure context).
Usage: python serve_https.py [port]  (default 8443, serves cwd)"""
import http.server
import os
import ssl
import sys

port = int(sys.argv[1]) if len(sys.argv) > 1 else 8443
os.chdir(os.path.dirname(os.path.abspath(__file__)))

ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain("cert.pem", "key.pem")

httpd = http.server.ThreadingHTTPServer(("0.0.0.0", port), http.server.SimpleHTTPRequestHandler)
httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
print(f"HTTPS serving on https://0.0.0.0:{port} (LAN: https://192.168.1.117:{port})")
httpd.serve_forever()
