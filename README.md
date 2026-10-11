# MeetStream MCP Server

**Give Claude - or any MCP-compatible client - direct access to the MeetStream meeting-bot API.** The server authenticates to MeetStream with your API key and exposes the full bot lifecycle (create, record, transcribe, summarize, interact live, manage calendar auto-join) as 19 callable tools.

Two ways to run it:

- **Remote (Streamable HTTP)** - the default, `https://mcp.meetstream.ai/mcp`. A hosted, multi-tenant endpoint you add by URL, with nothing to install. Sign in with OAuth from your client, or send an API key (see [Remote server](#remote-server-streamable-http) below).
- **Local (stdio)** - the `meetstream-mcp` binary, built from [`go/`](./go). Your MCP client launches it as a subprocess, and the key stays on your machine.

```
Remote:  Claude/MCP client ──HTTPS + OAuth or key──► mcp.meetstream.ai ──HTTPS + your key──► api.meetstream.ai
Local:   Claude/MCP client ──stdio (subprocess)────► meetstream-mcp ────HTTPS + API key───► api.meetstream.ai
```

Both run the same Go server. The old Node package `@meetstream/mcp` on npm is frozen at 0.3.0 and is no longer updated: it predates the current `create_bot` defaults and fixes, so do not use `npx @meetstream/mcp`.

---

## 1. Get an API key

Create one at **[app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys)**. This is the credential the server uses to authenticate every request - internally it sends `Authorization: Token <your-key>` on every call to `https://api.meetstream.ai/api/v1`. There's no separate MCP account: **the API key *is* the authentication.** With the hosted server you can skip this step and [sign in with OAuth](#sign-in-with-oauth-no-key-to-copy) instead; the dashboard then creates a key for that connection.

## 2. Install & configure

Using the hosted server? Skip to [Remote server](#remote-server-streamable-http): add the URL and sign in. The rest of this section is for running it locally.

Install the binary (needs [Go](https://go.dev/dl/) 1.25 or later):
```bash
go install github.com/meetstream-ai/meetstream-mcp/go/cmd/meetstream-mcp@latest
# installs to $(go env GOPATH)/bin/meetstream-mcp
```

The local server reads the key from the **`MEETSTREAM_API_KEY`** environment variable - you set it once in your MCP client's config and every tool call uses it automatically. Desktop apps often do not see your shell's `PATH`, so give them the binary's absolute path (`$(go env GOPATH)/bin/meetstream-mcp`, for example `/Users/you/go/bin/meetstream-mcp`).

### Claude Code (CLI)
```bash
claude mcp add meetstream --env MEETSTREAM_API_KEY=ms_XXXX -- meetstream-mcp
```

### Claude Desktop
Edit `claude_desktop_config.json` (Settings → Developer → Edit Config):
```json
{
  "mcpServers": {
    "meetstream": {
      "command": "/Users/you/go/bin/meetstream-mcp",
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
      "command": "/Users/you/go/bin/meetstream-mcp",
      "env": { "MEETSTREAM_API_KEY": "ms_XXXX" }
    }
  }
}
```

### Windsurf
Settings (`Cmd+,`) → **Cascade → MCP Servers → Add Server** - same `command`/`env` as above, written to `~/.codeium/windsurf/mcp_config.json`.

### Any other MCP client
Point it at the stdio command `meetstream-mcp` with `MEETSTREAM_API_KEY` set in the process environment. That's the entire integration surface - the server needs no other config.

### Verify it's connected
Ask your client something like *"list my meetstream bots"* - if it returns data (or a clean empty list) instead of an error, auth is working. A misconfigured key surfaces as a tool error: *"MEETSTREAM_API_KEY is not set..."* or a 401 from the API.

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

## Configuration

| Env var | Required | Purpose |
|---------|----------|---------|
| `MEETSTREAM_API_KEY` | ✅ yes (stdio mode) | Your API key - sent as `Authorization: Token <key>` on every request |
| `MEETSTREAM_API_URL` | optional | Override the base URL (default `https://api.meetstream.ai/api/v1`) - useful for testing against a staging environment |

---

## Remote server (Streamable HTTP)

`https://mcp.meetstream.ai/mcp` - a hosted, multi-tenant [Streamable HTTP](https://modelcontextprotocol.io) endpoint. Nothing to install on your machine.

### Sign in with OAuth (no key to copy)

Add the URL with no key. Your MCP client discovers MeetStream's OAuth server, opens a browser page, and stores the token itself:

```bash
claude mcp add --transport http meetstream https://mcp.meetstream.ai/mcp
# then run /mcp in Claude Code and choose Authenticate
```

In Claude (web or desktop), Cursor, VS Code and other OAuth-capable clients, add the same URL as a custom connector and click **Connect**. Under the hood this is the standard MCP authorization flow: protected resource metadata (RFC 9728), authorization server metadata (RFC 8414), dynamic client registration (RFC 7591) and the authorization code flow with PKCE. Access tokens last an hour and refresh automatically. To disconnect, delete the key named after the app on the [API keys page](https://app.meetstream.ai/api-key).

### Or send your API key

Every pre-OAuth method still works, unchanged:

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

`X-MeetStream-Api-Key: <key>` works too, and for clients whose connector UI cannot set headers, `https://mcp.meetstream.ai/mcp?key=<key>`. The server never logs query strings, but a key in a URL can still end up in client-side history, so prefer OAuth or a header where you can.

### Legacy SSE transport

Clients that predate Streamable HTTP (the 2024-11-05 HTTP+SSE transport) connect to `https://mcp.meetstream.ai/sse` with the same key options. The stream's first event names the per-session message endpoint; the key never appears in it.

### How it works

The server holds **no API key of its own**: each request (or SSE session) carries the caller's key or an OAuth token that wraps it. A request with no credentials gets a `401` with setup instructions and a `WWW-Authenticate` header pointing OAuth clients at the sign-in flow. Streamable HTTP is stateless, so the service scales horizontally with no shared state; OAuth tokens are encrypted, self-contained values, so there is no token database either.

The server is the Go implementation in [`go/`](./go): one shared server instance, about 1.7 ms of CPU per request. The same binary serves stdio (local) and HTTP (hosted). The Node implementation was removed from `main`; its last version is the [`node-v0.3.3`](https://github.com/meetstream-ai/meetstream-mcp/tree/node-v0.3.3) tag.

**Self-hosting?** See [`deploy/`](./deploy), or:
```bash
docker build -f go/Dockerfile -t meetstream-mcp go
docker run -p 8080:8080 meetstream-mcp            # POST http://localhost:8080/mcp
```
To enable OAuth on your own deployment set `MCP_OAUTH_MODE=paste`, `MCP_PUBLIC_URL` and `MCP_OAUTH_SECRET` (generate one with `docker run --rm meetstream-mcp gen-secret`). All settings are listed at the top of [`go/cmd/meetstream-mcp/main.go`](./go/cmd/meetstream-mcp/main.go).

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Tool call returns "MEETSTREAM_API_KEY is not set" | Add `MEETSTREAM_API_KEY` to the `env` block in your MCP client config, then restart the client |
| Tool call returns a 401 error | Your key is invalid or revoked - generate a new one at [app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys) |
| Client shows "meetstream" server failed to start | Run `MEETSTREAM_API_KEY=ms_XXXX meetstream-mcp` in a terminal - errors print to stderr. If the client cannot find the binary, give it the absolute path (`$(go env GOPATH)/bin/meetstream-mcp`) |
| `get_transcript` returns `ready: false` | The meeting hasn't finished processing yet, or (for streaming providers) there is no post-call transcript - check `get_bot_status` first |
| Calendar tools return empty/errors | No calendar is connected yet - connect one via `meetstream calendar connect` in the [CLI](https://github.com/meetstream-ai/meetstream-cli) first |
| Remote server (`mcp.meetstream.ai`) returns 401 | Sign in through your client (OAuth), or send `Authorization: Bearer <key>` / `X-MeetStream-Api-Key` - the remote server has no key of its own |

## Development

```bash
cd go
go test -race ./...                       # HTTP, OAuth, SSE, payload and real-client tests; no key, no network
MEETSTREAM_API_KEY=ms_... go run ./cmd/meetstream-mcp        # stdio
PORT=3000 go run ./cmd/meetstream-mcp http                   # Streamable HTTP + legacy SSE on :3000
```

---

Prefer a terminal? See the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli). Docs: [docs.meetstream.ai](https://docs.meetstream.ai) · API spec: [openapi.json](https://docs.meetstream.ai/openapi.json) · Migrating from Recall.ai: [@meetstream/migrate](https://github.com/meetstream-ai/recall-meetstream-migration-kit)

MIT © MeetStream.ai

## Telemetry

This server sends **anonymous** usage events (which tools are called, remote vs local) to help us improve MeetStream. It never sends your API key, meeting URLs, transcripts, or any content. Disable it any time:

```bash
export MEETSTREAM_TELEMETRY=0   # or the standard DO_NOT_TRACK=1
```
