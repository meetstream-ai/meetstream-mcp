// End-to-end smoke test: spawn the real stdio server, speak JSON-RPC to it.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const BIN = join(dirname(fileURLToPath(import.meta.url)), '..', 'bin', 'meetstream-mcp.js');

function rpc(proc, msg) {
  proc.stdin.write(JSON.stringify(msg) + '\n');
}

function collectResponses(proc, count, timeoutMs = 10_000) {
  return new Promise((resolve, reject) => {
    const out = [];
    let buf = '';
    const timer = setTimeout(() => reject(new Error(`timeout; got ${out.length}/${count}: ${buf.slice(0, 400)}`)), timeoutMs);
    proc.stdout.on('data', (d) => {
      buf += d.toString();
      let idx;
      while ((idx = buf.indexOf('\n')) !== -1) {
        const line = buf.slice(0, idx).trim();
        buf = buf.slice(idx + 1);
        if (!line) continue;
        try {
          const m = JSON.parse(line);
          if (m.id !== undefined) out.push(m);
          if (out.length >= count) { clearTimeout(timer); resolve(out); }
        } catch { /* ignore non-JSON lines */ }
      }
    });
    proc.on('error', reject);
  });
}

test('stdio server: initialize, tools/list, and a no-network tools/call', async () => {
  const proc = spawn(process.execPath, [BIN], {
    env: { ...process.env, MEETSTREAM_API_KEY: 'test_key_not_used' },
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  try {
    const responses = collectResponses(proc, 3);
    rpc(proc, { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'smoke', version: '0' } } });
    // give the server a beat to reply before follow-ups
    await new Promise((r) => setTimeout(r, 300));
    rpc(proc, { jsonrpc: '2.0', method: 'notifications/initialized' });
    rpc(proc, { jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} });
    rpc(proc, { jsonrpc: '2.0', id: 3, method: 'tools/call', params: { name: 'webhook_events_guide', arguments: {} } });

    const [init, list, call] = await responses;

    assert.equal(init.result.serverInfo.name, 'meetstream');

    const names = list.result.tools.map((t) => t.name).sort();
    const expected = [
      'create_bot', 'delete_bot_data', 'get_bot_detail', 'get_bot_status', 'get_bot_summary',
      'get_chats', 'get_media_urls', 'get_participants', 'get_speaker_timeline', 'get_transcript',
      'list_bots', 'list_calendar_events', 'list_transcriptions', 'remove_bot',
      'schedule_calendar_bot', 'send_chat_message', 'send_image', 'transcribe_audio', 'webhook_events_guide',
    ];
    for (const e of expected) assert.ok(names.includes(e), `missing tool: ${e}`);

    const text = call.result.content[0].text;
    assert.match(text, /"event" key/);
    assert.match(text, /bot\.stopped/);
    assert.match(text, /transcript_id is NOT in any webhook/);
  } finally {
    proc.kill();
  }
});
