import { createServer } from 'node:http';

const daemonURL = process.env.CLERM_DAEMON_URL ?? 'http://127.0.0.1:8181';
const listenPort = Number(process.env.PORT ?? '8383');

function jsonResponse(res: any, status: number, value: unknown) {
  const body = JSON.stringify(value);
  res.writeHead(status, { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) });
  res.end(body);
}

async function decodeRequest(payload: Buffer, target: string) {
  const response = await fetch(`${daemonURL}/v1/requests/decode`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/clerm',
      'Clerm-Target': target,
    },
    body: payload,
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return response.json();
}

async function encodeResponse(method: string, outputs: Record<string, unknown>) {
  const response = await fetch(`${daemonURL}/v1/responses/encode`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ method, outputs }),
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return Buffer.from(await response.arrayBuffer());
}

const server = createServer(async (req, res) => {
  if (req.method !== 'POST' || req.url !== '/api') {
    res.writeHead(404);
    res.end();
    return;
  }
  if ((req.headers['content-type'] ?? '').split(';')[0].trim() !== 'application/clerm') {
    jsonResponse(res, 415, { error: 'expected Content-Type: application/clerm' });
    return;
  }
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  try {
    const payload = Buffer.concat(chunks);
    const command: any = await decodeRequest(payload, String(req.headers['clerm-target'] ?? 'internal.search'));
    let outputs: Record<string, unknown>;
    switch (command.method) {
      case '@global.healthcare.search_providers.v1':
        outputs = {
          request_id: '123e4567-e89b-12d3-a456-426614174000',
          providers: [{ id: 'provider-1', name: 'Cardio Clinic' }],
        };
        break;
      case '@verified.healthcare.book_visit.v1':
        outputs = { order_id: 'visit-001', status: 'confirmed' };
        break;
      default:
        jsonResponse(res, 404, { error: `no handler for ${command.method}` });
        return;
    }
    const encoded = await encodeResponse(command.method, outputs);
    res.writeHead(200, {
      'Content-Type': 'application/clerm',
      'Content-Length': encoded.length,
      'Clerm-Method': command.method,
    });
    res.end(encoded);
  } catch (error) {
    jsonResponse(res, 500, { error: error instanceof Error ? error.message : 'unknown error' });
  }
});

server.listen(listenPort, '127.0.0.1', () => {
  console.log(`typescript sample listening on 127.0.0.1:${listenPort}`);
});
