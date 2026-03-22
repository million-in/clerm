import json
import os
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DAEMON_URL = os.environ.get('CLERM_DAEMON_URL', 'http://127.0.0.1:8181')
LISTEN_ADDR = os.environ.get('LISTEN_ADDR', '127.0.0.1')
PORT = int(os.environ.get('PORT', '8484'))


def decode_request(payload: bytes, target: str) -> dict:
    request = urllib.request.Request(
        f'{DAEMON_URL}/v1/requests/decode',
        data=payload,
        method='POST',
        headers={
            'Content-Type': 'application/clerm',
            'Clerm-Target': target,
        },
    )
    with urllib.request.urlopen(request) as response:
        return json.loads(response.read().decode('utf-8'))


def encode_response(method: str, outputs: dict) -> bytes:
    request = urllib.request.Request(
        f'{DAEMON_URL}/v1/responses/encode',
        data=json.dumps({'method': method, 'outputs': outputs}).encode('utf-8'),
        method='POST',
        headers={'Content-Type': 'application/json'},
    )
    with urllib.request.urlopen(request) as response:
        return response.read()


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != '/api':
            self.send_error(404)
            return
        if self.headers.get('Content-Type', '').split(';')[0].strip() != 'application/clerm':
            self.send_error(415, 'expected Content-Type: application/clerm')
            return
        length = int(self.headers.get('Content-Length', '0'))
        payload = self.rfile.read(length)
        try:
            command = decode_request(payload, self.headers.get('Clerm-Target', 'internal.search'))
            if command['method'] == '@global.healthcare.search_providers.v1':
                outputs = {
                    'request_id': '123e4567-e89b-12d3-a456-426614174000',
                    'providers': [{'id': 'provider-1', 'name': 'Cardio Clinic'}],
                }
            elif command['method'] == '@verified.healthcare.book_visit.v1':
                outputs = {'order_id': 'visit-001', 'status': 'confirmed'}
            else:
                self.send_error(404, f"no handler for {command['method']}")
                return
            encoded = encode_response(command['method'], outputs)
            self.send_response(200)
            self.send_header('Content-Type', 'application/clerm')
            self.send_header('Content-Length', str(len(encoded)))
            self.send_header('Clerm-Method', command['method'])
            self.end_headers()
            self.wfile.write(encoded)
        except Exception as error:
            self.send_error(500, str(error))


if __name__ == '__main__':
    server = ThreadingHTTPServer((LISTEN_ADDR, PORT), Handler)
    print(f'python sample listening on {LISTEN_ADDR}:{PORT}')
    server.serve_forever()
