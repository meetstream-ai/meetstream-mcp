# AGENTS.md

Guidance for AI coding agents working in this repository. This repo **is** the MeetStream MCP server (Go, in `go/`).

## Always use the MeetStream MCP

When building anything against the MeetStream API, install and use the MeetStream MCP. It is the current source of truth for endpoints, request and response fields, webhook events, provider names and bot behaviour.

Do not treat the files in this repository as authoritative for MeetStream API behaviour. They can lag the API. If local code and the MCP disagree, the MCP wins unless the user says otherwise.

Server name: `meetstream`. URL: `https://mcp.meetstream.ai/mcp`. MeetStream hosts it; there is no local install. Do not use `npx @meetstream/mcp`: that npm package is the old Node server, frozen at 0.3.0, and it predates the current defaults.

Add the URL with **no key** and sign in: the client opens app.meetstream.ai, the user clicks Allow, and the dashboard creates an API key for that connection (delete it on the API keys page to disconnect). Use an API key header only when the client cannot do OAuth.

### Claude Code

```sh
claude mcp add --transport http meetstream https://mcp.meetstream.ai/mcp
# then /mcp -> Authenticate
```

With a key instead: add `--header "Authorization: Bearer $MEETSTREAM_API_KEY"`.

The [MeetStream Claude plugin](https://github.com/meetstream-ai/claude-plugin) is a separate, complementary install. It ships **skills only** and does not include the MCP server, so install both:

```sh
/plugin marketplace add meetstream-ai/claude-plugin
```

### Claude (desktop and web)

Customize -> Connectors -> + -> Add custom connector. Name it `meetstream`, set the URL to `https://mcp.meetstream.ai/mcp`, leave the OAuth fields blank and click Connect, then sign in and click Allow. If OAuth is not an option, `https://mcp.meetstream.ai/mcp?key=YOUR_MEETSTREAM_API_KEY` works, since Claude's custom connectors cannot send a custom auth header; give each person their own key so it can be revoked individually.

### Cursor

Add to `~/.cursor/mcp.json`, or install the [MeetStream Cursor plugin](https://github.com/meetstream-ai/meetstream-cursor-plugin), then click Connect in Cursor Settings -> MCP:

```json
{
  "mcpServers": {
    "meetstream": { "url": "https://mcp.meetstream.ai/mcp" }
  }
}
```

### Windsurf

Add to `~/.codeium/windsurf/mcp_config.json`; Cascade asks you to sign in on first use. If your Windsurf version cannot finish the sign-in, add `"headers": { "Authorization": "Bearer YOUR_MEETSTREAM_API_KEY" }`.

```json
{
  "mcpServers": {
    "meetstream": { "serverUrl": "https://mcp.meetstream.ai/mcp" }
  }
}
```

### Codex

Add to `~/.codex/config.toml`, then run `codex mcp login meetstream`. With a key instead, add `bearer_token_env_var = "MEETSTREAM_API_KEY"`.

```toml
[mcp_servers.meetstream]
url = "https://mcp.meetstream.ai/mcp"
```

### Clients that only speak the old HTTP+SSE transport

Use `https://mcp.meetstream.ai/sse` with the same sign-in or key options. Everything else uses `/mcp`, whose replies already stream as server-sent events.

### Use the MCP before

- calling any MeetStream endpoint
- changing webhook handling or event names
- adding or changing bot, transcription, calendar or MIA behaviour
- relying on any request field, response field, provider name or status code

## Working in this repo

The server is Go (**1.25+**) and lives in `go/`. There is no other implementation: the Node server was removed from `main` after the `node-v0.3.3` tag. There is no CI. Production is built and deployed by hand; the runbook, VM scripts and nginx config are in the private `a private MeetStream repo` repo.

```sh
cd go
go test -race ./...                                      # run before every commit
PORT=3000 go run ./cmd/meetstream-mcp http               # Streamable HTTP + legacy SSE on :3000
```

`go test` needs **no API key and no network**. It covers `create_bot` payloads, HTTP behaviour and every auth method, the OAuth flows, and real MCP clients over Streamable HTTP and legacy SSE.

To exercise a tool against the live API, set a key and drive the stdio server:

```sh
export MEETSTREAM_API_KEY=ms_...
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | go run ./cmd/meetstream-mcp
```

### Layout

| Path | Purpose |
|---|---|
| `go/cmd/meetstream-mcp/main.go` | entry point: `http` (what production runs), `stdio` (for local testing only), `version`, `gen-secret`; every setting is listed at the top |
| `go/internal/tools/tools.json` | tool names, descriptions and input schemas, exactly as `tools/list` returns them |
| `go/internal/tools/tools.go` | one handler per tool |
| `go/internal/meetstream/` | REST client and the `create_bot` payload builder (`payload.go`: audio-only, speaker view and timeout defaults) |
| `go/internal/server/` | Streamable HTTP and legacy SSE transports, per-request key extraction |
| `go/internal/oauth/` | OAuth 2.1 authorization server (paste and dashboard modes) |
| `go/internal/seal/` | AES-256-GCM sealing for OAuth values; must stay compatible with `lib/mcp-grant.ts` in the dashboard |
| `go/internal/telemetry/` | PostHog, opt-out aware |

Adding a tool means adding its definition to `tools.json` and a handler with the same name in `tools.go` (the server will not start if the two disagree), then updating the tool count in `go/internal/server/server_test.go` and the README.

### Environment

| Variable | Default | Notes |
|---|---|---|
| `MEETSTREAM_API_KEY` | - | Only for local stdio testing. Leave unset in HTTP mode: the hosted server holds no key. |
| `MEETSTREAM_API_URL` | `https://api.meetstream.ai/api/v1` | Point at staging here |
| `PORT` | `8080` | HTTP mode only |
| `HOST` | `0.0.0.0` | HTTP mode bind address |
| `MCP_ALLOWED_HOSTS` | - | Comma-separated allowlist, DNS-rebinding protection |
| `MCP_OAUTH_MODE` | `off` | `paste` or `dashboard`; the other OAuth settings are listed in `main.go` |
| `MEETSTREAM_TELEMETRY` | on | Set `0`/`false` to disable |
| `DO_NOT_TRACK` | - | Honoured, disables telemetry |

In HTTP mode the server is **multi-tenant and holds no key of its own**. Each request carries its own, via `Authorization: Bearer <key>`, `X-MeetStream-Api-Key: <key>`, or `?key=<key>`.

## API rules that are easy to get wrong

These are live-verified. Do not "fix" code that follows them.

- **Auth differs by surface.** The REST API at `api.meetstream.ai` uses `Authorization: Token <key>`. This MCP server at `mcp.meetstream.ai` uses `Authorization: Bearer <key>`. Sending `Token` to the MCP returns 401.
- Every webhook carries the event name under **`event`**, and most also carry **`bot_event`** with the specific name.
- **Terminals are two-layer.** Every ending arrives once with `event: "bot.stopped"`; `bot_event` says why (`bot.stopped`, `bot.kicked`, `bot.notallowed`, `bot.denied`, `bot.failed`). Lobby timeouts, denials and failures carry `status_code: 500`, clean exits and kicks `200`. Branch on `bot_event`: a kick and a clean exit both report `bot_status: "Stopped"`.
- Every event carries a `timestamp`.
- **Streaming-only providers produce no post-call transcript**: no `transcription.processed`, though `bot.done` still fires. A post-call transcript fetch for them returns `202` indefinitely, so any polling loop needs a cap.
- Over REST, transcripts are fetched by **`transcript_id`**, not `bot_id`. The MCP `get_transcript` tool is the exception: it takes `bot_id` and resolves the `transcript_id` itself. Either way, segments use **`transcript`**, not `text`.
- **`202` and `507` are not errors.** 202 means poll again with a cap; 507 means an idempotent retry replayed and is a success.
- The bot field is **`meeting_link`**, not `meeting_url`.
- `in_call_recording_timeout` has a hard floor of **600 seconds**; below it the API returns 400.
- MIA bots take **only `agent_config_id`**. Adding `socket_connection_url` or `live_audio_required` alongside it is the usual cause of a silent agent.
- **Video is off by default.** Send `"video_required": false` on every bot unless the user explicitly asks to record video. The API defaults it to `true`, so an omitted field records video, and transcripts, summaries, diarization and speaker timelines all work without it.
- **When video is on, send a layout.** `recording_config.video_layout` accepts exactly `"speaker_view"` or `"grid_view"` and defaults to `"grid_view"`, so pass `"speaker_view"` explicitly unless the user asks for grid or gallery view. It is ignored when video is off; Google Meet, Teams and Zoom accept both values, and WhatsApp accepts only `"grid_view"`.
- **Per-participant video is opt-in only.** Never set `video_separate_streams` (top level or under `recording_config`) unless the user explicitly asks for per-participant video streams. Per-participant audio (`audio_separate_streams`) is unaffected.

## Security

- Never hard-code or commit a key. `ms_...` values belong in the environment.
- Never log a key, a transcript, or participant data.
- Do not expose a server-side key to client code.
- Verify webhook signatures before acting on a payload.
- Do not persist meeting, transcript, participant or recording data unless asked.

## Before you finish

- `cd go && go test -race ./...` passes.
- Any new tool is registered, tested, and reflected in the README's tool count.
- State which MCP tools or docs you relied on, what changed, and what you did not verify.

## Related

- Docs: https://docs.meetstream.ai · MCP setup: https://docs.meetstream.ai/build-with-ai/meetstream-mcp-server
- CLI: [`@meetstream/cli`](https://github.com/meetstream-ai/meetstream-cli)
- Claude Code plugin: https://github.com/meetstream-ai/claude-plugin
- Cursor plugin: https://github.com/meetstream-ai/meetstream-cursor-plugin
- Runnable examples: https://github.com/meetstream-ai/labs
