# AGENTS.md

Guidance for AI coding agents working in this repository. This repo **is** the MeetStream MCP server (`@meetstream/mcp`).

## Always use the MeetStream MCP

When building anything against the MeetStream API, install and use the MeetStream MCP. It is the current source of truth for endpoints, request and response fields, webhook events, provider names and bot behaviour.

Do not treat the files in this repository as authoritative for MeetStream API behaviour. They can lag the API. If local code and the MCP disagree, the MCP wins unless the user says otherwise.

Server name: `meetstream`. Hosted URL: `https://mcp.meetstream.ai/mcp`.

### Claude Code

```sh
claude mcp add --transport http meetstream https://mcp.meetstream.ai/mcp \
  --header "Authorization: Bearer $MEETSTREAM_API_KEY"
```

The [MeetStream Claude plugin](https://github.com/meetstream-ai/claude-plugin) is a separate, complementary install. It ships **skills only** and does not include the MCP server, so install both:

```sh
/plugin marketplace add meetstream-ai/claude-plugin
```

### Cursor

Add to `~/.cursor/mcp.json`, or install the [MeetStream Cursor plugin](https://github.com/meetstream-ai/meetstream-cursor-plugin) which wires this up for you:

```json
{
  "mcpServers": {
    "meetstream": {
      "url": "https://mcp.meetstream.ai/mcp",
      "headers": { "Authorization": "Bearer YOUR_MEETSTREAM_API_KEY" }
    }
  }
}
```

### Windsurf

Add to `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "meetstream": {
      "serverUrl": "https://mcp.meetstream.ai/mcp",
      "headers": { "Authorization": "Bearer YOUR_MEETSTREAM_API_KEY" }
    }
  }
}
```

### Claude (desktop and web)

Customize -> Connectors -> + -> Add custom connector. Name it `meetstream` and set the URL to `https://mcp.meetstream.ai/mcp?key=YOUR_MEETSTREAM_API_KEY`, leaving the OAuth fields blank. Claude's custom connectors cannot send a custom auth header, so the key goes in the URL; give each person their own key so it can be revoked individually.

### Codex

Add to `~/.codex/config.toml` (the key is read from your environment):

```toml
[mcp_servers.meetstream]
url = "https://mcp.meetstream.ai/mcp"
bearer_token_env_var = "MEETSTREAM_API_KEY"
```

### Run it locally instead

Any client that speaks stdio can run the server straight from npm, which keeps the key on your machine:

```sh
MEETSTREAM_API_KEY=ms_... npx -y @meetstream/mcp
```

### Use the MCP before

- calling any MeetStream endpoint
- changing webhook handling or event names
- adding or changing bot, transcription, calendar or MIA behaviour
- relying on any request field, response field, provider name or status code

## Working in this repo

Node **>= 18.17**, ESM (`"type": "module"`), zero build step. Source runs as written.

```sh
npm install

npm start            # stdio server - what MCP clients spawn
npm run start:http   # Streamable HTTP server on PORT (default 3000)
npm test             # node --test over test/*.test.js
```

`npm test` needs **no API key and no network**. `test/smoke.test.js` spawns the real stdio binary and speaks JSON-RPC to it; `test/http.test.js` exercises the HTTP transport. Run it before every commit.

To exercise a tool against the live API, set a key and drive the stdio server:

```sh
export MEETSTREAM_API_KEY=ms_...
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | node bin/meetstream-mcp.js
```

### Layout

| Path | Purpose |
|---|---|
| `bin/meetstream-mcp.js` | stdio entry point |
| `bin/meetstream-mcp-http.js` | Streamable HTTP entry point |
| `src/server.js` | tool definitions and registration |
| `src/api.js` | REST client, base URL, auth header |
| `src/http-server.js` | HTTP transport, per-request key extraction |
| `src/telemetry.js` | PostHog, opt-out aware |
| `test/` | `node --test` suites |

Adding a tool means editing `src/server.js` (schema + handler) and `src/api.js` (the call), then extending `test/smoke.test.js` so the tool count assertion stays honest.

### Environment

| Variable | Default | Notes |
|---|---|---|
| `MEETSTREAM_API_KEY` | - | Required for real calls. Get one at https://app.meetstream.ai/api-key |
| `MEETSTREAM_API_URL` | `https://api.meetstream.ai/api/v1` | Point at staging here |
| `PORT` | `3000` | HTTP transport only |
| `HOST` | - | HTTP transport bind address |
| `MCP_ALLOWED_HOSTS` | - | Comma-separated allowlist, DNS-rebinding protection |
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

- `npm test` passes.
- Any new tool is registered, tested, and reflected in the README's tool count.
- State which MCP tools or docs you relied on, what changed, and what you did not verify.

## Related

- Docs: https://docs.meetstream.ai · MCP setup: https://docs.meetstream.ai/build-with-ai/meetstream-mcp-server
- CLI: [`@meetstream/cli`](https://github.com/meetstream-ai/meetstream-cli)
- Claude Code plugin: https://github.com/meetstream-ai/claude-plugin
- Cursor plugin: https://github.com/meetstream-ai/meetstream-cursor-plugin
- Runnable examples: https://github.com/meetstream-ai/labs
