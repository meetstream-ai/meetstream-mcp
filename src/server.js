// MeetStream MCP server — exposes the MeetStream meeting-bot API as MCP tools.
// Ground truth: https://docs.meetstream.ai/openapi.json + live-verified webhook model.
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { z } from 'zod';
import { MeetStreamClient, buildCreateBotPayload } from './api.js';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { version } = require('../package.json');

const PROVIDERS = ['deepgram', 'assemblyai', 'sarvam', 'meetstream', 'jigsawstack', 'meeting_captions', 'deepgram_streaming', 'assemblyai_streaming'];

const WEBHOOK_GUIDE = `MeetStream webhook model (LIVE-VERIFIED against production):

Envelope — events POST to your callback_url with the name under the "event" key:
{ "event": "bot.inmeeting", "bot_id": "...", "bot_status": "InMeeting",
  "message": "...", "status_code": 200, "custom_attributes": {...} }
status_code: 200 success, 500 failure. Lifecycle events have NO timestamp; post-call events do.

Lifecycle: bot.joining (may fire up to 3x) -> bot.in_waiting_room -> bot.inmeeting ->
bot.recording -> bot.leaving -> bot.stopped (terminal, fires ONCE).
TWO-LAYER: on bot.stopped, bot_status says WHY: Stopped (normal) | NotAllowed (lobby timeout) |
Denied (host denied) | Error (crash). There are NOT separate kicked/denied/failed events.
bot.error = NON-terminal streaming-provider upstream error (bot continues; no status_code).

Post-call: manifest.completed -> audio.processed -> transcription.processed (or
transcription.failed, 500) -> video.processed (only if video_required) -> bot.done (200 or 500).
STREAMING-ONLY transcription providers (deepgram_streaming, assemblyai_streaming,
meeting_captions) never fire transcription.processed/failed or bot.done — their terminal
event is audio.processed. Don't wait on bot.done for a streaming bot.

transcript_id is NOT in any webhook. Resolve it via GET /bots/{id}/detail
(bot_details.transcript_id), the create_bot response, or GET /bots/{id}/transcriptions —
the get_transcript tool does this automatically.

bot_status values: Joining, InWaitingRoom, InMeeting, Recording, Leaving, Stopped,
NotAllowed, Denied, Error, Done.

Handler rules: return HTTP 2xx fast (webhooks are NOT retried on non-2xx); process async;
recording_permission_denied_timeout minimum is 60 seconds (Zoom only).`;

function json(data) {
  return { content: [{ type: 'text', text: typeof data === 'string' ? data : JSON.stringify(data, null, 2) }] };
}
function errText(e) {
  return { content: [{ type: 'text', text: `MeetStream API error: ${e.message}` }], isError: true };
}

export function createServer({ apiKey = process.env.MEETSTREAM_API_KEY, fetchImpl } = {}) {
  const server = new McpServer({ name: 'meetstream', version });
  let _client = null;
  const client = () => {
    if (!_client) {
      if (!apiKey) throw new Error('MEETSTREAM_API_KEY is not set. Create a key at https://app.meetstream.ai/api-keys and set it in this server\'s environment.');
      _client = new MeetStreamClient(apiKey, fetchImpl ? { fetchImpl } : {});
    }
    return _client;
  };
  const run = (fn) => async (args) => {
    try { return json(await fn(args)); } catch (e) { return errText(e); }
  };

  // ── Bot lifecycle ─────────────────────────────────────────────────────
  server.registerTool('create_bot', {
    title: 'Create a meeting bot',
    description: 'Send a MeetStream bot to a Zoom / Google Meet / Microsoft Teams meeting (or schedule it with join_at). Returns bot_id and (when a transcription provider is set) transcript_id. Set callback_url to receive lifecycle webhooks.',
    inputSchema: {
      meeting_link: z.string().describe('Full meeting URL (Zoom, Google Meet, or Teams)'),
      bot_name: z.string().optional().describe('Display name in the meeting (default "MeetStream Bot")'),
      record_video: z.boolean().optional().describe('Record video too (default false = audio only)'),
      transcription_provider: z.enum(PROVIDERS).optional().describe('Post-call: deepgram (default choice), assemblyai, sarvam (Indic), meetstream, jigsawstack, meeting_captions (native). Real-time: deepgram_streaming, assemblyai_streaming. NOTE: streaming providers never fire transcription.processed/bot.done webhooks.'),
      language: z.string().optional().describe('Language in the provider\'s format (deepgram "en", assemblyai "en_us", sarvam "en-IN")'),
      callback_url: z.string().optional().describe('HTTPS webhook for lifecycle events (events arrive under the "event" key)'),
      join_at: z.string().optional().describe('Schedule a future join, ISO 8601 e.g. 2026-07-02T15:00:00Z'),
      bot_message: z.string().optional().describe('Chat message posted when the bot joins'),
      bot_image_url: z.string().optional().describe('PUBLIC image URL for the bot avatar (raw base64 is rejected)'),
      retention_hours: z.number().int().optional().describe('Data retention window in hours (API default 24)'),
      separate_audio_streams: z.boolean().optional().describe('Capture per-participant audio'),
      separate_video_streams: z.boolean().optional().describe('Capture per-participant video'),
      agent_config_id: z.string().optional().describe('Attach a MIA conversational AI agent'),
      live_transcript_webhook_url: z.string().optional().describe('Webhook URL for live transcript chunks'),
      custom_attributes: z.record(z.string()).optional().describe('String key/values echoed back in every webhook'),
      idempotency_key: z.string().optional().describe('UUID for safe retries (a retry returns the original bot, HTTP 507, no double charge)'),
    },
  }, run(async (a) => {
    const payload = buildCreateBotPayload({
      meetingLink: a.meeting_link, name: a.bot_name, video: a.record_video,
      transcript: a.transcription_provider, language: a.language, callback: a.callback_url,
      joinAt: a.join_at, botMessage: a.bot_message, imageUrl: a.bot_image_url,
      retentionHours: a.retention_hours, separateAudio: a.separate_audio_streams,
      separateVideo: a.separate_video_streams, agentConfigId: a.agent_config_id,
      liveTranscriptWebhook: a.live_transcript_webhook_url, attrs: a.custom_attributes,
    });
    const { status, data } = await client().createBot(payload, { idempotencyKey: a.idempotency_key });
    return { ...data, idempotent_replay: status === 507 || undefined, sent_payload: payload };
  }));

  server.registerTool('list_bots', {
    title: 'List bots',
    description: 'List all bots on the account (paginated: bots[], hasNextPage, nextCursor).',
    inputSchema: {},
    annotations: { readOnlyHint: true },
  }, run(async () => (await client().listBots()).data));

  server.registerTool('get_bot_status', {
    title: 'Get bot status',
    description: 'Current bot status. Values: Joining, InWaitingRoom, InMeeting, Recording, Leaving, Stopped, NotAllowed, Denied, Error, Done.',
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().botStatus(a.bot_id)).data));

  server.registerTool('get_bot_detail', {
    title: 'Get bot detail',
    description: 'Full session metadata: platform, duration, timings, status timeline, transcript_id (canonical source), caption_file (for meeting_captions provider), original request payload.',
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().botDetail(a.bot_id)).data));

  server.registerTool('get_bot_summary', {
    title: 'Get AI meeting summary',
    description: "MeetStream's built-in AI summary of the meeting (no external LLM needed).",
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().botSummary(a.bot_id)).data));

  server.registerTool('remove_bot', {
    title: 'Remove bot from meeting',
    description: 'Make the bot leave an active meeting now. Recorded data is KEPT (use delete_bot_data to erase).',
    inputSchema: { bot_id: z.string() },
  }, run(async (a) => (await client().removeBot(a.bot_id)).data));

  server.registerTool('delete_bot_data', {
    title: 'Delete bot data (permanent)',
    description: 'PERMANENTLY delete a bot\'s audio, video, and transcripts. Irreversible — fires a data_deletion webhook. Only call when the user explicitly asks to delete data.',
    inputSchema: { bot_id: z.string(), confirm: z.literal(true).describe('Must be true — confirms the user explicitly asked for permanent deletion') },
    annotations: { destructiveHint: true },
  }, run(async (a) => (await client().deleteBotData(a.bot_id)).data));

  // ── Transcripts ───────────────────────────────────────────────────────
  server.registerTool('get_transcript', {
    title: 'Get transcript',
    description: 'Fetch a bot\'s transcript by bot_id. Resolves transcript_id automatically (it is NOT in webhooks) via /detail → /transcriptions. Set wait=true to poll until ready (after transcription.processed fires). Segments have `speaker` and `transcript` fields.',
    inputSchema: {
      bot_id: z.string(),
      wait: z.boolean().optional().describe('Poll until the transcript is ready (up to timeout_seconds)'),
      timeout_seconds: z.number().int().optional().describe('Max wait when wait=true (default 300)'),
      raw: z.boolean().optional().describe('Return raw provider output instead of processed segments'),
    },
    annotations: { readOnlyHint: true },
  }, run(async (a) => {
    const r = await client().getTranscript(a.bot_id, {
      wait: Boolean(a.wait), raw: Boolean(a.raw),
      timeoutMs: (a.timeout_seconds ?? 300) * 1000,
    });
    if (r.transcript == null) {
      return { ready: false, transcript_id: r.transcript_id, hint: 'Transcript not ready yet. Wait for the transcription.processed webhook or call again with wait=true. Streaming-only providers never produce a post-call transcript.' };
    }
    return { ready: true, transcript_id: r.transcript_id, segments: r.transcript };
  }));

  server.registerTool('list_transcriptions', {
    title: 'List transcription runs',
    description: 'All transcription runs for a bot: transcript_id, provider, status, presigned download_urls (valid 1h).',
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().transcriptions(a.bot_id)).data));

  server.registerTool('transcribe_audio', {
    title: 'Run / re-run transcription',
    description: 'Start a (re-)transcription of a bot\'s recorded audio with a chosen post-call provider. Useful to try a different provider or language after the meeting.',
    inputSchema: {
      bot_id: z.string(),
      provider: z.enum(['deepgram', 'assemblyai', 'sarvam', 'meetstream', 'jigsawstack']).default('deepgram'),
      language: z.string().optional(),
      callback_url: z.string().optional().describe('Webhook to notify when done'),
    },
  }, run(async (a) => {
    const p = {};
    if (a.provider === 'deepgram') p.deepgram = { model: 'nova-3', language: a.language || 'en' };
    else if (a.provider === 'assemblyai') p.assemblyai = { speech_models: ['best'], language_code: a.language || 'en_us' };
    else if (a.provider === 'sarvam') p.sarvam = { model: 'saarika:v2', language_code: a.language || 'en-IN', mode: 'batch' };
    else if (a.provider === 'meetstream') p.meetstream = { language: a.language || 'auto', translate: false };
    else p.jigsawstack = { language: a.language || 'auto', translate: false };
    return (await client().transcribe(a.bot_id, p, a.callback_url)).data;
  }));

  // ── Media + meeting data ──────────────────────────────────────────────
  server.registerTool('get_media_urls', {
    title: 'Get recording URLs',
    description: 'Presigned S3 URLs for recorded media. kind=audio (valid 1h), video (valid 10min), audio_streams / video_streams (per-participant; require separate_*_streams at creation), screenshots.',
    inputSchema: { bot_id: z.string(), kind: z.enum(['audio', 'video', 'audio_streams', 'video_streams', 'screenshots']).default('audio') },
    annotations: { readOnlyHint: true },
  }, run(async (a) => {
    const c = client();
    const map = { audio: 'botAudio', video: 'botVideo', audio_streams: 'audioStreams', video_streams: 'recordingStreams', screenshots: 'screenshots' };
    return (await c[map[a.kind]](a.bot_id)).data;
  }));

  server.registerTool('get_participants', {
    title: 'Get participants',
    description: 'Participants detected in the meeting (displayName, fullName, status, stream ids).',
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().participants(a.bot_id)).data));

  server.registerTool('get_chats', {
    title: 'Get in-meeting chat',
    description: 'Chat messages captured during the meeting.',
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().chats(a.bot_id)).data));

  server.registerTool('get_speaker_timeline', {
    title: 'Get speaker timeline',
    description: 'Who spoke and when (chunk timeline with speaker ids/names).',
    inputSchema: { bot_id: z.string() },
    annotations: { readOnlyHint: true },
  }, run(async (a) => (await client().speakerTimeline(a.bot_id)).data));

  // ── Live-meeting interaction ──────────────────────────────────────────
  server.registerTool('send_chat_message', {
    title: 'Send chat message into the meeting',
    description: 'Post a chat message into the live meeting through the bot.',
    inputSchema: { bot_id: z.string(), message: z.string() },
  }, run(async (a) => (await client().sendMessage(a.bot_id, a.message)).data));

  server.registerTool('send_image', {
    title: 'Show an image in the meeting',
    description: 'Display an image/GIF as the bot\'s video frame. img_url must be PUBLIC.',
    inputSchema: { bot_id: z.string(), img_url: z.string(), display_duration_seconds: z.number().int().optional() },
  }, run(async (a) => (await client().sendImage(a.bot_id, a.img_url, a.display_duration_seconds)).data));

  // ── Calendar ──────────────────────────────────────────────────────────
  server.registerTool('list_calendar_events', {
    title: 'List calendar events',
    description: 'Upcoming events from connected Google Calendars (connect via POST /calendar/create_calendar with google_client_id/secret/refresh_token — needs OAuth credentials, usually done once from the dashboard or CLI).',
    inputSchema: {},
    annotations: { readOnlyHint: true },
  }, run(async () => (await client().calendarEvents()).data));

  server.registerTool('schedule_calendar_bot', {
    title: 'Schedule / unschedule a calendar bot',
    description: 'action=schedule sends a bot to a specific calendar event; action=unschedule removes it.',
    inputSchema: { event_id: z.string(), action: z.enum(['schedule', 'unschedule']).default('schedule') },
  }, run(async (a) => (a.action === 'schedule'
    ? (await client().scheduleEvent(a.event_id)).data
    : (await client().unscheduleEvent(a.event_id)).data)));

  // ── Reference ─────────────────────────────────────────────────────────
  server.registerTool('webhook_events_guide', {
    title: 'Webhook events guide (live-verified)',
    description: 'Authoritative reference for MeetStream webhook events — envelope shape, full event list, two-layer bot.stopped model, streaming-provider caveats. Use this before writing any webhook handler.',
    inputSchema: {},
    annotations: { readOnlyHint: true },
  }, async () => json(WEBHOOK_GUIDE));

  return server;
}
