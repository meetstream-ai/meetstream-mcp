# MeetStream MCP Server

**Give Claude - or any MCP-compatible client - direct access to the MeetStream meeting-bot API.** Add one URL, sign in, and your assistant can run the full bot lifecycle (create, record, transcribe, summarize, interact live, manage calendar auto-join) through 19 tools.

```
https://mcp.meetstream.ai/mcp
```

MeetStream hosts the server. There is nothing to install or run on your machine, and it is always the current version.

```
Claude/MCP client ──HTTPS (OAuth sign-in or API key)──► mcp.meetstream.ai ──► api.meetstream.ai
```

> The old npm package `@meetstream/mcp` (`npx @meetstream/mcp`) is frozen at 0.3.0 and no longer maintained. It predates the current `create_bot` defaults and fixes. Use the hosted URL instead.

---

## Connect

Add the URL with **no key**. Your client finds MeetStream's sign-in, opens `app.meetstream.ai` in your browser, and you click **Allow**. The dashboard creates an API key for that connection; to disconnect, delete the key named after the app on the [API keys page](https://app.meetstream.ai/api-key).

### Claude Code
```bash
claude mcp add --transport http meetstream https://mcp.meetstream.ai/mcp
# then run /mcp in Claude Code and choose Authenticate
```

### Claude (web and desktop)
Customize → **Connectors** → **+** → **Add custom connector**. Name it `meetstream`, set the URL to `https://mcp.meetstream.ai/mcp`, leave the OAuth fields blank, and click **Connect**.

### Cursor
`~/.cursor/mcp.json` (or install the [MeetStream Cursor plugin](https://github.com/meetstream-ai/meetstream-cursor-plugin)), then click **Connect** next to `meetstream` in Cursor Settings → MCP:
```json
{
  "mcpServers": {
    "meetstream": { "url": "https://mcp.meetstream.ai/mcp" }
  }
}
```

### VS Code
`.vscode/mcp.json`, then start the server and sign in when VS Code asks:
```json
{
  "servers": {
    "meetstream": { "type": "http", "url": "https://mcp.meetstream.ai/mcp" }
  }
}
```

### Windsurf
`~/.codeium/windsurf/mcp_config.json`. Cascade asks you to sign in on first use. If your Windsurf version cannot finish the sign-in, use an API key header (below):
```json
{
  "mcpServers": {
    "meetstream": { "serverUrl": "https://mcp.meetstream.ai/mcp" }
  }
}
```

### Codex
`~/.codex/config.toml`, then run `codex mcp login meetstream`:
```toml
[mcp_servers.meetstream]
url = "https://mcp.meetstream.ai/mcp"
```

### Any other MCP client
Point it at `https://mcp.meetstream.ai/mcp` as a remote (Streamable HTTP) server. Clients that support MCP authorization sign in by themselves: protected resource metadata (RFC 9728), authorization server metadata (RFC 8414), dynamic client registration (RFC 7591) and the authorization code flow with PKCE. Access tokens last an hour and refresh automatically.

### Or use an API key

For clients without OAuth, or for scripts and CI, create a key at **[app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys)** and send it as a header:

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

- Claude Code: `claude mcp add --transport http meetstream https://mcp.meetstream.ai/mcp --header "Authorization: Bearer $MEETSTREAM_API_KEY"`
- Codex: add `bearer_token_env_var = "MEETSTREAM_API_KEY"` to the `[mcp_servers.meetstream]` block.
- `X-MeetStream-Api-Key: <key>` works too. For connector UIs that cannot set headers, `https://mcp.meetstream.ai/mcp?key=<key>`. The server never logs query strings, but a key in a URL can end up in client-side history, so prefer OAuth or a header.

### Older clients (HTTP+SSE)

`/mcp` is the main endpoint, and its replies already stream as server-sent events (`text/event-stream`). Clients that only speak the older 2024-11-05 HTTP+SSE transport connect to `https://mcp.meetstream.ai/sse` with the same sign-in or key options. The stream's first event names the per-session message endpoint; the key never appears in it.

### Check it is connected
Ask your client something like *"list my meetstream bots"* - if it returns data (or a clean empty list) instead of an error, you are connected.

---

## Then just ask

> "Join my standup at https://meet.google.com/abc-defg-hij, record it with Deepgram transcription, and give me the transcript when it's done."

> "List my bots from today and give me the AI summary of the 3pm customer call."

> "Send 'we'll follow up by email' into the meeting the bot is in, then show them our logo."

> "Schedule a bot for tomorrow's board meeting on my calendar."

---

## Full capability list - 19 tools

### Bot lifecycle
| Tool | What it does |
|------|---------------|
| `create_bot` | Sends (or schedules, via `join_at`) a bot to a Zoom / Google Meet / Microsoft Teams meeting. Audio only by default; `record_video` turns video on and records it in speaker view unless `video_layout: "grid_view"` is asked for, and per-participant video stays off unless requested. Configurable: transcription provider + language, `callback_url` for webhooks, video recording, per-participant audio/video streams, MIA conversational agent, signed-in joins (`google_login_domain` / `teams_login_domain`, optional `sign_in_email` / `strict_email`, for domains already registered on your account), authenticated Zoom joins (`zoom_zak_url` / `zoom_obf_url`), custom attributes, retention window, idempotency key (safe retries - a repeat call returns the original bot, never a duplicate). Returns `bot_id` and `transcript_id` (when a provider is set). |
| `list_bots` | Lists every bot on the account (paginated). |
| `get_bot_status` | Current lifecycle status - one of `Joining`, `InWaitingRoom`, `InMeeting`, `Recording`, `Leaving`, `Stopped`, `NotAllowed`, `Denied`, `Error`, `Done`. |
| `get_bot_detail` | Full session metadata: platform, timings, status timeline, the canonical `transcript_id`, `caption_file` (for the `meeting_captions` provider), and the original request payload. |
| `get_bot_summary` | MeetStream's built-in AI meeting summary - no external LLM call needed. |
| `remove_bot` | Makes the bot leave an active meeting immediately. Recorded data is **kept**. |
| `delete_bot_data` | **Permanently** deletes a bot's audio, video, and transcripts. Requires `confirm: true` - only call this when the user has explicitly asked to delete data. Irreversible. |

### Transcription
| Tool | What it does |
|------|---------------|
| `get_transcript` | Fetches a transcript by `bot_id`. Automatically resolves the `transcript_id` (it is never delivered in webhooks) via `/detail` → `/transcriptions`. Set `wait: true` to poll until it's ready; `raw: true` for unprocessed provider output. Segments come back as `{ speaker, transcript, start_time, end_time }`. |
| `list_transcriptions` | Lists every transcription run for a bot - provider, status, and presigned download URLs (valid 1h). |
| `transcribe_audio` | (Re-)transcribes a bot's recorded audio with a chosen provider - useful to retry with a different provider or language after the meeting. |

### Media & meeting data
| Tool | What it does |
|------|---------------|
| `get_media_urls` | Presigned URLs for `audio` (1h), `video` (10min), `audio_streams`/`video_streams` (per-participant - needs `separate_audio_streams`/`separate_video_streams` at creation), or `screenshots`. |
| `get_participants` | Everyone detected in the meeting - display name, full name, status, stream ids. |
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
| `list_calendar_events` | Upcoming events from a connected Google Calendar. (Connecting a calendar - `POST /calendar/create_calendar` with OAuth credentials - is a one-time setup usually done via the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli) or dashboard.) |
| `schedule_calendar_bot` | Schedules (`action: "schedule"`) or removes (`"unschedule"`) a bot for a specific calendar event. |

### Reference
| Tool | What it does |
|------|---------------|
| `webhook_events_guide` | Returns the webhook reference - envelope shape (`event` and `bot_event`), the two-layer `bot.stopped` terminal model, status codes, and streaming-provider caveats, checked against captured production deliveries. **Have your model call this before it writes any webhook handler code.** |

---

## What makes this different from just reading the docs

Every tool description and the `webhook_events_guide` bake in **live-verified ground truth**, confirmed against real production bot runs, that the public API docs currently get wrong:

- **Every delivery carries `event`**, and most also carry `bot_event` with the specific name. Read `event` to route, `bot_event` for detail.
- **Terminals are two-layer**: every ending arrives once with `event: "bot.stopped"`, and `bot_event` says why (`bot.stopped`, `bot.kicked`, `bot.notallowed`, `bot.denied`, `bot.failed`). Lobby timeouts, denials and failures carry `status_code: 500`. Branch on `bot_event`, not `bot_status` - a kick and a clean exit both report `Stopped`.
- **Streaming-only transcription providers** (`deepgram_streaming`, `assemblyai_streaming`, `jigsawstack_streaming`, `meetstream_streaming`, `meeting_captions`) produce no post-call transcript, so `transcription.processed` never fires. `bot.done` still does. A post-call transcript fetch for them returns `202` indefinitely, so cap your polling.
- **`transcript_id` is never in a webhook payload** - `get_transcript` resolves it for you automatically instead of making the model guess.
- Safe defaults everywhere: `automatic_leave` timeouts on every `create_bot` call, and `recording_permission_denied_timeout` floored at 60 (the API rejects lower values with a 400).

## How it works

The server holds **no API key of its own**: each request (or SSE session) carries the caller's key or an OAuth token that wraps it. A request with no credentials gets a `401` with setup instructions and a `WWW-Authenticate` header pointing OAuth clients at the sign-in flow. Streamable HTTP is stateless, so the service scales horizontally with no shared state; OAuth tokens are encrypted, self-contained values, so there is no token database either.

The server is the Go implementation in [`go/`](./go): one shared server instance, about 1.7 ms of CPU per request.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Sign-in page says the link has expired | Sign-in links last 15 minutes. Start the connection again from your client |
| `401` from `mcp.meetstream.ai` | Sign in through your client (OAuth), or send `Authorization: Bearer <key>` / `X-MeetStream-Api-Key` - the server has no key of its own |
| Tool call returns a 401 from the API | The key is invalid or revoked (for OAuth connections, the key named after the app was deleted). Reconnect, or generate a new key at [app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys) |
| `get_transcript` returns `ready: false` | The meeting hasn't finished processing yet, or (for streaming providers) there is no post-call transcript - check `get_bot_status` first |
| Calendar tools return empty/errors | No calendar is connected yet - connect one via `meetstream calendar connect` in the [CLI](https://github.com/meetstream-ai/meetstream-cli) first |
| Your client only supports the old SSE transport | Use `https://mcp.meetstream.ai/sse` |

## Development

For working on the server itself, not for connecting to it:

```bash
cd go
go test -race ./...                              # HTTP, OAuth, SSE, payload and real-client tests; no key, no network
PORT=3000 go run ./cmd/meetstream-mcp http       # Streamable HTTP + legacy SSE on :3000
```

---

Prefer a terminal? See the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli). Docs: [docs.meetstream.ai](https://docs.meetstream.ai) · API spec: [openapi.json](https://docs.meetstream.ai/openapi.json) · Migrating from Recall.ai: [@meetstream/migrate](https://github.com/meetstream-ai/recall-meetstream-migration-kit)

MIT © MeetStream.ai

## Telemetry

This server sends **anonymous** usage events (which tools are called, remote vs local) to help us improve MeetStream. It never sends your API key, meeting URLs, transcripts, or any content. Disable it any time:

```bash
export MEETSTREAM_TELEMETRY=0   # or the standard DO_NOT_TRACK=1
```
