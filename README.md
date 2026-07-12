# MeetStream MCP Server

**Give Claude — or any MCP-compatible client — direct access to the MeetStream meeting-bot API.** The server authenticates to MeetStream with your API key and exposes the full bot lifecycle (create, record, transcribe, summarize, interact live, manage calendar auto-join) as 19 callable tools.

Two ways to run it:

- **Local (stdio)** — the default, `npx @meetstream/mcp`. Your MCP client launches it as a subprocess; nothing to host.
- **Remote (Streamable HTTP)** — `https://mcp.meetstream.ai/mcp`. A hosted, multi-tenant endpoint you can add by URL, no local install. Each request authenticates with its own API key via header (see [Remote server](#remote-server-streamable-http) below).

```
Local:   Claude/MCP client ──stdio (subprocess)──► @meetstream/mcp ──HTTPS + API key──► api.meetstream.ai
Remote:  Claude/MCP client ──HTTPS + your key───► mcp.meetstream.ai ──HTTPS + your key──► api.meetstream.ai
```

---

## 1. Get an API key

Create one at **[app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys)**. This is the credential the server uses to authenticate every request — internally it sends `Authorization: Token <your-key>` on every call to `https://api.meetstream.ai/api/v1`. There's no OAuth flow, no login step, no separate MCP account: **the API key *is* the authentication.**

## 2. Install & configure

The server reads the key from the **`MEETSTREAM_API_KEY`** environment variable — you set it once in your MCP client's config and every tool call uses it automatically.

### Claude Code (CLI)
```bash
claude mcp add meetstream --env MEETSTREAM_API_KEY=ms_XXXX -- npx -y @meetstream/mcp
```

### Claude Desktop
Edit `claude_desktop_config.json` (Settings → Developer → Edit Config):
```json
{
  "mcpServers": {
    "meetstream": {
      "command": "npx",
      "args": ["-y", "@meetstream/mcp"],
      "env": { "MEETSTREAM_API_KEY": "ms_XXXX" }
    }
  }
}
```
Restart Claude Desktop after saving.

### Cursor
Settings → **Cursor Settings → MCP → Add new global MCP server**, or edit `~/.cursor/mcp.json`:
```json
{
  "mcpServers": {
    "meetstream": {
      "command": "npx",
      "args": ["-y", "@meetstream/mcp"],
      "env": { "MEETSTREAM_API_KEY": "ms_XXXX" }
    }
  }
}
```

### Windsurf
Settings (`Cmd+,`) → **Cascade → MCP Servers → Add Server** — same `command`/`args`/`env` as above, written to `~/.codeium/windsurf/mcp_config.json`.

### Any other MCP client
Point it at the stdio command `npx -y @meetstream/mcp` with `MEETSTREAM_API_KEY` set in the process environment. That's the entire integration surface — the server needs no other config.

### Verify it's connected
Ask your client something like *"list my meetstream bots"* — if it returns data (or a clean empty list) instead of an error, auth is working. A misconfigured key surfaces as a tool error: *"MEETSTREAM_API_KEY is not set..."* or a 401 from the API.

---

## Then just ask

> "Join my standup at https://meet.google.com/abc-defg-hij, record it with Deepgram transcription, and give me the transcript when it's done."

> "List my bots from today and give me the AI summary of the 3pm customer call."

> "Send 'we'll follow up by email' into the meeting the bot is in, then show them our logo."

> "Schedule a bot for tomorrow's board meeting on my calendar."

---

## Full capability list — 19 tools

### Bot lifecycle
| Tool | What it does |
|------|---------------|
| `create_bot` | Sends (or schedules, via `join_at`) a bot to a Zoom / Google Meet / Microsoft Teams meeting. Configurable: transcription provider + language, `callback_url` for webhooks, video recording, per-participant audio/video streams, MIA conversational agent, custom attributes, retention window, idempotency key (safe retries — a repeat call returns the original bot, never a duplicate). Returns `bot_id` and `transcript_id` (when a provider is set). |
| `list_bots` | Lists every bot on the account (paginated). |
| `get_bot_status` | Current lifecycle status — one of `Joining`, `InWaitingRoom`, `InMeeting`, `Recording`, `Leaving`, `Stopped`, `NotAllowed`, `Denied`, `Error`, `Done`. |
| `get_bot_detail` | Full session metadata: platform, timings, status timeline, the canonical `transcript_id`, `caption_file` (for the `meeting_captions` provider), and the original request payload. |
| `get_bot_summary` | MeetStream's built-in AI meeting summary — no external LLM call needed. |
| `remove_bot` | Makes the bot leave an active meeting immediately. Recorded data is **kept**. |
| `delete_bot_data` | **Permanently** deletes a bot's audio, video, and transcripts. Requires `confirm: true` — only call this when the user has explicitly asked to delete data. Irreversible. |

### Transcription
| Tool | What it does |
|------|---------------|
| `get_transcript` | Fetches a transcript by `bot_id`. Automatically resolves the `transcript_id` (it is never delivered in webhooks) via `/detail` → `/transcriptions`. Set `wait: true` to poll until it's ready; `raw: true` for unprocessed provider output. Segments come back as `{ speaker, transcript, start_time, end_time }`. |
| `list_transcriptions` | Lists every transcription run for a bot — provider, status, and presigned download URLs (valid 1h). |
| `transcribe_audio` | (Re-)transcribes a bot's recorded audio with a chosen provider — useful to retry with a different provider or language after the meeting. |

### Media & meeting data
| Tool | What it does |
|------|---------------|
| `get_media_urls` | Presigned URLs for `audio` (1h), `video` (10min), `audio_streams`/`video_streams` (per-participant — needs `separate_audio_streams`/`separate_video_streams` at creation), or `screenshots`. |
| `get_participants` | Everyone detected in the meeting — display name, full name, status, stream ids. |
| `get_chats` | In-meeting chat messages captured during the call. |
| `get_speaker_timeline` | Who spoke and when, as a timeline of speaker segments. |

### Live meeting interaction
| Tool | What it does |
|------|---------------|
| `send_chat_message` | Posts a chat message into the live meeting through the bot. |
| `send_image` | Displays an image or GIF as the bot's video frame. `img_url` must be publicly accessible (no base64). |

### Calendar
| Tool | What it does |
|------|---------------|
| `list_calendar_events` | Upcoming events from a connected Google Calendar. (Connecting a calendar — `POST /calendar/create_calendar` with OAuth credentials — is a one-time setup usually done via the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli) or dashboard.) |
| `schedule_calendar_bot` | Schedules (`action: "schedule"`) or removes (`"unschedule"`) a bot for a specific calendar event. |

### Reference
| Tool | What it does |
|------|---------------|
| `webhook_events_guide` | Returns the live-verified webhook reference — envelope shape, the full event list, the two-layer `bot.stopped`/`bot_status` model, and streaming-provider caveats. **Have your model call this before it writes any webhook handler code** — the public docs page has known inaccuracies this tool corrects. |

---

## What makes this different from just reading the docs

Every tool description and the `webhook_events_guide` bake in **live-verified ground truth**, confirmed against real production bot runs, that the public API docs currently get wrong:

- **Webhook envelope key is `event`**, not `bot_event` as the docs claim.
- **`bot.stopped` is two-layer**: it fires exactly once, and `bot_status` (`Stopped`/`NotAllowed`/`Denied`/`Error`) tells you why — there's no separate `bot.kicked`/`bot.denied` event.
- **Streaming-only transcription providers** (`deepgram_streaming`, `assemblyai_streaming`, `meeting_captions`) never fire `transcription.processed` or `bot.done` — their terminal event is `audio.processed`. A handler waiting on `bot.done` for a streaming bot will hang forever.
- **`transcript_id` is never in a webhook payload** — `get_transcript` resolves it for you automatically instead of making the model guess.
- Safe defaults everywhere: `automatic_leave` timeouts on every `create_bot` call, and `recording_permission_denied_timeout` floored at 60 (the API rejects lower values with a 400).

## Configuration

| Env var | Required | Purpose |
|---------|----------|---------|
| `MEETSTREAM_API_KEY` | ✅ yes (stdio mode) | Your API key — sent as `Authorization: Token <key>` on every request |
| `MEETSTREAM_API_URL` | optional | Override the base URL (default `https://api.meetstream.ai/api/v1`) — useful for testing against a staging environment |

---

## Remote server (Streamable HTTP)

`https://mcp.meetstream.ai/mcp` — a hosted, multi-tenant [Streamable HTTP](https://modelcontextprotocol.io) endpoint. No `npx`, no local Node, no per-machine install. Add it by URL:

```json
{
  "mcpServers": {
    "meetstream": {
      "url": "https://mcp.meetstream.ai/mcp",
      "headers": { "Authorization": "Bearer ms_YOUR_API_KEY" }
    }
  }
}
```

**How auth works here is different from stdio mode:** this endpoint serves many different MeetStream accounts at once, so it holds **no API key of its own**. Every request must carry your key, either as `Authorization: Bearer <key>` or `X-MeetStream-Api-Key: <key>`. A request with no key gets a `401` with setup instructions instead of silently failing.

The server is stateless — every request is independent, there's no session to keep alive, and it scales horizontally with zero shared state between requests.

**Self-hosting it yourself?** The same code ships as a Docker image — see [`deploy/`](./deploy) for the full runbook (fresh isolated VM, nginx, Let's Encrypt, systemd) or just:
```bash
docker build -t meetstream-mcp .
docker run -p 8080:8080 meetstream-mcp   # POST http://localhost:8080/mcp
```

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Tool call returns "MEETSTREAM_API_KEY is not set" | Add `MEETSTREAM_API_KEY` to the `env` block in your MCP client config, then restart the client |
| Tool call returns a 401 error | Your key is invalid or revoked — generate a new one at [app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys) |
| Client shows "meetstream" server failed to start | Run `npx -y @meetstream/mcp` directly in a terminal — errors will print to stderr |
| `get_transcript` returns `ready: false` | The meeting hasn't finished processing yet, or (for streaming providers) there is no post-call transcript — check `get_bot_status` first |
| Calendar tools return empty/errors | No calendar is connected yet — connect one via `meetstream calendar connect` in the [CLI](https://github.com/meetstream-ai/meetstream-cli) first |
| Remote server (`mcp.meetstream.ai`) returns 401 | You didn't send `Authorization: Bearer <key>` (or `X-MeetStream-Api-Key`) on the request — the remote server has no key of its own |

## Development

```bash
npm install
npm test        # spawns the real stdio server and speaks JSON-RPC to it end-to-end
```

---

Prefer a terminal? See the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli). Docs: [docs.meetstream.ai](https://docs.meetstream.ai) · API spec: [openapi.json](https://docs.meetstream.ai/openapi.json) · Migrating from Recall.ai: [@meetstream/migrate](https://github.com/meetstream-ai/recall-meetstream-migration-kit)

MIT © MeetStream.ai
