// End-to-end test for the remote Streamable HTTP transport: real server, real HTTP requests.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { app } from '../src/http-server.js';

// StreamableHTTP may respond as plain JSON or as a single-event SSE stream
// (`event: message\ndata: {...}\n\n`) depending on content negotiation — both are spec-valid.
async function readMcpResponse(res) {
  const contentType = res.headers.get('content-type') || '';
  if (contentType.includes('text/event-stream')) {
    const text = await res.text();
    const line = text.split('\n').find((l) => l.startsWith('data:'));
    return JSON.parse(line.slice(5).trim());
  }
  return res.json();
}

async function withServer(fn) {
  const server = app.listen(0, '127.0.0.1');
  await new Promise((resolve) => server.once('listening', resolve));
  const { port } = server.address();
  try {
    await fn(`http://127.0.0.1:${port}`);
  } finally {
    server.close();
  }
}

test('GET / returns server info', async () => {
  await withServer(async (base) => {
    const res = await fetch(`${base}/`);
    assert.equal(res.status, 200);
    const body = await res.json();
    assert.equal(body.name, 'meetstream-mcp');
    assert.equal(body.mcp_endpoint, '/mcp');
  });
});

test('GET /health returns ok', async () => {
  await withServer(async (base) => {
    const res = await fetch(`${base}/health`);
    assert.equal(res.status, 200);
    assert.deepEqual(await res.json(), { status: 'ok' });
  });
});

test('POST /mcp without an API key returns 401 with guidance', async () => {
  await withServer(async (base) => {
    const res = await fetch(`${base}/mcp`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 't', version: '0' } } }),
    });
    assert.equal(res.status, 401);
    const body = await res.json();
    assert.match(body.error.message, /Authorization: Bearer/);
  });
});

test('POST /mcp with a Bearer key initializes and lists all 19 tools', async () => {
  await withServer(async (base) => {
    const init = await fetch(`${base}/mcp`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream', authorization: 'Bearer test_key_not_used' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 't', version: '0' } } }),
    });
    assert.equal(init.status, 200);
    const initBody = await readMcpResponse(init);
    assert.equal(initBody.result.serverInfo.name, 'meetstream');

    const list = await fetch(`${base}/mcp`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream', authorization: 'Bearer test_key_not_used' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} }),
    });
    assert.equal(list.status, 200);
    const listBody = await readMcpResponse(list);
    const names = listBody.result.tools.map((t) => t.name);
    assert.equal(names.length, 19);
    assert.ok(names.includes('create_bot'));
    assert.ok(names.includes('webhook_events_guide'));
  });
});

test('POST /mcp accepts X-MeetStream-Api-Key as an alternative to Authorization', async () => {
  await withServer(async (base) => {
    const res = await fetch(`${base}/mcp`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream', 'x-meetstream-api-key': 'test_key' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 't', version: '0' } } }),
    });
    assert.equal(res.status, 200);
  });
});

test('POST /mcp?key=... authenticates via query param (for connector-UI URLs)', async () => {
  await withServer(async (base) => {
    const res = await fetch(`${base}/mcp?key=test_key_in_url`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 't', version: '0' } } }),
    });
    assert.equal(res.status, 200);
    const body = await readMcpResponse(res);
    assert.equal(body.result.serverInfo.name, 'meetstream');
  });
});

test('GET /mcp (no session, stateless) returns 405', async () => {
  await withServer(async (base) => {
    const res = await fetch(`${base}/mcp`);
    assert.equal(res.status, 405);
  });
});
