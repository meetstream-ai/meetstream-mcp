/**
 * Anonymous, opt-out usage telemetry (PostHog).
 * Captures ONLY tool/command names + counts - never API keys, meeting URLs,
 * transcripts, or any content. Disable with DO_NOT_TRACK=1 or MEETSTREAM_TELEMETRY=0.
 * The project key is a PostHog *public* key.
 */
import crypto from 'node:crypto';
import os from 'node:os';

const KEY = 'phc_oFCXdmvQVdgxSwQoCG2GCwupkmt3UqGovj4feMp5ZuCq';
const HOST = 'https://us.i.posthog.com';

let _transport = 'unknown';
let _client = null;
let _loaded = false;

export function telemetryEnabled() {
  const dnt = process.env.DO_NOT_TRACK;
  const mt = process.env.MEETSTREAM_TELEMETRY;
  if (dnt === '1' || dnt === 'true') return false;
  if (mt === '0' || mt === 'false' || mt === 'off') return false;
  return true;
}

export function setTransport(t) { _transport = t; }

function anonId() {
  try {
    const raw = `${os.hostname()}|${os.userInfo().username || ''}|${os.homedir()}`;
    return 'anon_' + crypto.createHash('sha256').update(raw).digest('hex').slice(0, 16);
  } catch { return 'anon_unknown'; }
}

async function client() {
  if (!telemetryEnabled()) return null;
  if (_loaded) return _client;
  _loaded = true;
  try {
    const { PostHog } = await import('posthog-node');
    _client = new PostHog(KEY, { host: HOST, flushAt: 1, flushInterval: 0 });
  } catch { _client = null; }
  return _client;
}

export async function track(event, properties = {}) {
  try {
    const c = await client();
    if (!c) return;
    c.capture({
      distinctId: anonId(),
      event,
      properties: { transport: _transport, $lib: 'meetstream-mcp', ...properties },
    });
  } catch { /* telemetry must never throw */ }
}

export async function shutdownTelemetry() {
  try { if (_client) await _client.shutdown(); } catch { /* ignore */ }
}
