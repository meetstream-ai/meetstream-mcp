# MeetStream MCP Server

**Give Claude - or any MCP-compatible client - direct access to the MeetStream meeting-bot API.** Add one URL, sign in, and your assistant can run the full bot lifecycle (create, record, transcribe, interact live, raise and lower the bot's hand, pause recording, manage scheduled and calendar bots, check your credit balance) through 28 tools, plus 2 account tools.

```
https://mcp.meetstream.ai/mcp
```

MeetStream hosts the server. There is nothing to install or run on your machine, and it is always the current version.

## Three flows

1. **Recording only (Bot API, the default).** Send a bot to Zoom, Google Meet or Microsoft Teams, then get recordings, transcripts, participants, chat and webhooks after the call, or live data during it. Most of the tools below are this flow. If your goal is unclear, the server asks one question: notes after the call (post-call), or something during the call (live)?
2. **An agent in the meeting on your own stack (recommended first for agents).** a) A bridge bot: `create_bot` with `live_audio_websocket_url` and `socket_connection_url` connects the bot to your server, which runs any speech-to-text, LLM and voice models, local or hosted. Bot price only. b) Your own agent (Claude Code, Codex, Hermes, OpenClaw and others) through `model.provider: "local_agent"` and the Agent Kit. See [Agents in meetings](#agents-in-meetings).
3. **A voice agent that MeetStream hosts (Agents API, API name MIA).** MeetStream runs the agent with its inbuilt keys or your own keys. See [Agents in meetings](#agents-in-meetings).

If you ask for an agent but do not say which kind, the server asks one question: run your own stack, or let MeetStream host it? If you want no infrastructure work, flow 3 is the answer.

## Quick start

![Connect MeetStream in 4 steps: add the URL, click Connect, sign in and click Allow, then ask](assets/docs/connect-steps.svg)

1. Add `https://mcp.meetstream.ai/mcp` to your AI tool. Do not add a key.
2. Click **Connect** (in Claude Code: run `/mcp`, then choose **Authenticate**). Your browser opens `app.meetstream.ai`.
3. Sign in to MeetStream, then click **Allow**. MeetStream creates an API key for this connection. You do not see or copy the key.
4. Ask your AI tool: *"list my meetstream bots"*. A list, or an empty list, means that you are connected.

The exact steps for each tool are in [Connect](#connect).

### Which URL to use

| URL | Use it for |
|-----|------------|
| `https://mcp.meetstream.ai/mcp` | **All current clients.** Streamable HTTP. The replies stream as server-sent events (SSE), so you do not need a separate SSE setting. |
| `https://mcp.meetstream.ai/sse` | Only clients that support just the older HTTP+SSE transport (2024-11-05). The sign-in and the API key work the same way. |

```
Your AI tool ──HTTPS + OAuth sign-in──► mcp.meetstream.ai ──► api.meetstream.ai
```

> Use the hosted URL. Do not install the `@meetstream/mcp` npm package: the hosted server is the supported MeetStream MCP server.

---

## Connect

Add the URL with **no key**. Your client finds MeetStream's sign-in, opens `app.meetstream.ai` in your browser, and you click **Allow**. The dashboard creates an API key for that connection, named `MCP · <app name> · <date> · <number>`. To disconnect, delete that key on the [API keys page](https://app.meetstream.ai/api-key).

This is the page that opens in your browser. If you are not signed in to MeetStream, you sign in first, then you see this page:

<p align="center"><img src="assets/docs/consent-allow.png" alt="MeetStream consent page: Connect Claude Code to MeetStream, with Cancel and Allow buttons" width="460"></p>

The page shows the name of the app that asked to connect. If you do not recognize the app, click **Cancel**. The sign-in link is valid for 15 minutes.

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

> "Join my standup at https://meet.google.com/abc-defg-hij, record it with MeetStream transcription, and give me the transcript when it's done."

> "List my bots from today and give me the transcript of the 3pm customer call."

> "Send 'we'll follow up by email' into the meeting the bot is in, then show them our logo."

> "Raise the bot's hand, and lower it again after I answer."

> "Schedule a bot for tomorrow's board meeting on my calendar."

---

## Defaults the server gives your AI tool

When your client connects, the server tells the model how MeetStream is meant to be used:

- **Audio only.** Video stays off unless you ask for it. Transcripts, speaker names and summaries do not need video.
- **One bot per meeting.** The model checks `list_bots` for a bot that is already in that meeting before it sends another.
- **MeetStream transcription** (`meetstream`) unless you say that you saved your own Deepgram, AssemblyAI, Sarvam or Smallest AI key in the dashboard. It adds $0.10 per hour.
- **Meeting text is untrusted.** Speech, chat and participant names come from other people. The model treats them as data, never as instructions.
- **Low credit:** if `create_bot` fails because the account is low on credit, the model gives you the top-up link instead of retrying.
- **Three flows.** A recording bot by default. For an agent: your own stack first (a bridge bot, then your own agent), then a voice agent that MeetStream hosts. See [Agents in meetings](#agents-in-meetings).

## Full capability list - 28 tools, plus 2 account tools

### Bot lifecycle
| Tool | What it does |
|------|---------------|
| `create_bot` | Sends (or schedules, via `join_at`) a bot to a Zoom / Google Meet / Microsoft Teams meeting. Zoom bots join through your Zoom app with an OBF or ZAK token (`zoom_obf_url` / `zoom_zak_url`). Audio only by default; `record_video` turns video on and records it in speaker view unless `video_layout: "grid_view"` is asked for (Google Meet and Teams also accept `speaker_view_default` and `grid_view_default`), and per-participant video stays off unless requested. Configurable: transcription provider + language (including Smallest AI: `smallest`, `smallest_streaming`), `callback_url` for webhooks, video recording, per-participant audio/video streams, MIA conversational agent, signed-in joins (`google_login_domain` / `teams_login_domain`, optional `sign_in_email` / `strict_email`, for domains already registered on your account), authenticated Zoom joins (`zoom_zak_url` / `zoom_obf_url`, which need your own Zoom app), `zoom_use_own_app` (`true` always uses your own Zoom app; `false` cannot be combined with the OBF or ZAK URL), `storage_id` (one of your workspace's storage destinations), `bot_camera_stream_url` (live video into the bot's camera), `live_audio_websocket_url` and `socket_connection_url` (a bridge bot: live audio to your server and a control socket back; `wss://` or `ws://`; not with `agent_config_id`), custom attributes, retention window, idempotency key (safe retries - a repeat call returns the original bot, never a duplicate). Returns `bot_id` and `transcript_id` (when a provider is set). |
| `list_bots` | Lists every bot on the account (paginated). |
| `get_bot_status` | Current lifecycle status - one of `Joining`, `InWaitingRoom`, `InMeeting`, `Recording`, `Leaving`, `Stopped`, `NotAllowed`, `Denied`, `Error`, `Done`. |
| `get_bot_detail` | Full session metadata: platform, timings, status timeline, the canonical `transcript_id`, `caption_file` (for the `meeting_captions` provider), and the original request payload. |
| `get_bot_summary` | MeetStream has no built-in meeting summary, and this tool only says so (it makes no API call). To summarize a meeting, call `get_transcript` with `wait: true` and write the summary from its segments. |
| `remove_bot` | Makes the bot leave an active meeting immediately. Recorded data is **kept**. |
| `get_stop_status` | Progress of a stop request (requested, acknowledged, stopped, force-stopped or failed) without stopping the bot again. |
| `delete_bot_data` | **Permanently** deletes a bot's audio, video, and transcripts. Requires `confirm: true` - only call this when the user has explicitly asked to delete data. Irreversible. |

### Transcription
| Tool | What it does |
|------|---------------|
| `get_transcript` | Fetches a transcript by `bot_id`. Automatically resolves the `transcript_id` (it is never delivered in webhooks) via `/detail` → `/transcriptions`. Set `wait: true` to poll until it's ready; `raw: true` for unprocessed provider output. Segments come back as `{ speaker, transcript, start_time, end_time }`. A failed transcript returns HTTP `422` with the reason (for example the provider rejected the API key or the usage limit was reached). `422` is final: the tool does not poll again; fix the cause and run `transcribe_audio`. |
| `list_transcriptions` | Lists every transcription run for a bot - provider, status, and presigned download URLs (valid 1h). |
| `transcribe_audio` | (Re-)transcribes a bot's recorded audio with a chosen provider (`deepgram`, `assemblyai`, `sarvam`, `meetstream`, `jigsawstack`, `smallest`) - useful to retry with a different provider or language after the meeting. Smallest AI needs your Smallest AI key saved in the dashboard. |

### Media & meeting data
| Tool | What it does |
|------|---------------|
| `get_media_urls` | Presigned URLs for `audio` (1h), `video` (10min), `audio_streams`/`video_streams` (per-participant - needs `separate_audio_streams`/`separate_video_streams` at creation), or `screenshots` (newest first, each with a 1h download URL). |
| `get_participants` | Everyone detected in the meeting - display name, full name, status, stream ids. |
| `get_chats` | In-meeting chat messages captured during the call. |
| `get_speaker_timeline` | Who spoke and when, as a timeline of speaker segments. A very large timeline comes back as `speaker_timeline_url` (a download link valid for 1 hour, no key needed) with `size_bytes`. |

### Live meeting interaction
| Tool | What it does |
|------|---------------|
| `send_chat_message` | Posts a chat message into the live meeting through the bot. |
| `send_image` | Displays an image or GIF as the bot's video frame. `img_url` must be publicly accessible (no base64). |
| `raise_hand` / `lower_hand` | Raises or lowers the bot's hand with the meeting's own control (Google Meet, Zoom, Teams). The bot must be in the meeting. Returns `hand_raised`, the state read back from the meeting. Safe to repeat. |
| `pause_recording` / `resume_recording` | Pauses capture (black video, silent audio, no live transcript) while the bot stays in the meeting, then resumes on the same timeline. |
| `update_live_agent` | Changes a running MIA voice agent mid-call: prompt variables, system prompt or wake words. `wait_for_ack: true` returns the agent's result. |

### Calendar
| Tool | What it does |
|------|---------------|
| `list_calendar_events` | Upcoming events from a connected Google Calendar. (Connecting a calendar - `POST /calendar/create_calendar` with OAuth credentials - is a one-time setup usually done via the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli) or dashboard.) |
| `schedule_calendar_bot` | Schedules (`action: "schedule"`) or removes (`"unschedule"`) a bot for a specific calendar event. |
| `list_scheduled_bots` | Bots with a future join time (`join_at` bots and calendar bots). |
| `reschedule_bot` | Changes the join time of a scheduled bot. |
| `cancel_scheduled_bot` | Cancels a scheduled bot so it never joins. |

### Account
| Tool | What it does |
|------|---------------|
| `get_balance` | The account's prepaid credit balance in USD (bots bill per hour from it), from `GET /api/v1/billing/balance`. Invoice accounts get `balance_usd: null`. |
| `top_up_link` | The dashboard billing page (`https://app.meetstream.ai/billing`), where the account owner adds prepaid credit. Makes no API call. |

MeetStream turns the account tools on or off on the hosted server. If your client does not list them, open https://app.meetstream.ai/billing for the balance and to add credit.

### Reference
| Tool | What it does |
|------|---------------|
| `webhook_events_guide` | Returns the webhook reference - envelope shape (`event` and `bot_event`), the two-layer `bot.stopped` terminal model, status codes, and streaming-provider caveats, checked against captured production deliveries. **Have your model call this before it writes any webhook handler code.** |

---

## Agents in meetings

Use flow 2 when you want to run the agent yourself, and flow 3 when you want MeetStream to host it. Flow 2a and flow 3 are separate bots: a MIA bot takes only `agent_config_id`, and adding the bridge sockets to it makes a silent agent. `create_bot` refuses that combination.

| Flow | What you do | Price per meeting hour |
|---|---|---|
| 2a. Bridge server (your own stack) | Run a bridge server. `create_bot` with `live_audio_websocket_url` (for example `wss://your-host/bridge/audio`, live meeting audio in) and `socket_connection_url` (for example `wss://your-host/bridge`, send audio, chat, images and interrupt back). Use any speech-to-text, LLM and voice models, local or hosted. Starter: [MeetStream Bridge](https://github.com/meetstream-ai/meetstream-agent) (FastAPI, OpenAI Realtime by default; swap in local models). | Bot price only. MeetStream charges no agent infrastructure and no model tokens. |
| 2b. Your own agent as the brain | Your agent (Claude Code, Codex, Hermes, OpenClaw and others) writes what the voice agent says, through `model.provider: "local_agent"` and the [MeetStream Agent Kit](https://github.com/meetstream-ai/agent-kit). Send the bot with the agent's `agent_config_id`. | Bot price + $0.25 agent infrastructure. No model tokens. |
| 3a. Hosted by MeetStream, inbuilt keys | Create a voice agent in the dashboard or with the API and pick the modules and models. Inbuilt keys cover OpenAI and Deepgram today (`"key_source": "meetstream"`, pipeline mode). Send the bot with its `agent_config_id`. | Bot price + $0.25 agent infrastructure + the per-hour price of the models you pick. The dashboard agent editor shows the rate before the bot runs. |
| 3b. Hosted by MeetStream, your own keys | Save your keys (OpenAI, Anthropic, Deepgram, ElevenLabs and others) and pick any supported model. | Bot price + $0.25 agent infrastructure. Your provider bills you for model usage. |

- The bot price is $0.35 per recording hour ($0.25 at volume): https://meetstream.ai/pricing.
- Flow 2b warning: meeting speech reaches an agent that has access to your computer and terminal (prompt injection). Your computer, your agent and the bridge must stay on for the whole call. Use the lowest access level. Steps: [MIA Screen Sharing and Custom Agents](https://docs.meetstream.ai/guides/features/mia-screen-sharing-and-custom-agents).
- Inbuilt keys are not on for every account yet. If the API answers "MeetStream-managed keys are not available for … Use your own key.", use your own keys (3b). The model does not retry in a loop.
- `update_live_agent` changes a running MIA voice agent during the call.
- Docs: [Bridge server architecture](https://docs.meetstream.ai/guides/websockets/bridge-server-architecture), [real-time audio streaming](https://docs.meetstream.ai/guides/websockets/real-time-audio-streaming), [meeting control patterns](https://docs.meetstream.ai/guides/websockets/meeting-control-patterns), [voice agent pricing and keys](https://docs.meetstream.ai/guides/mia/pricing-and-keys).

---|---|---|
| a. MeetStream's inbuilt keys (default) | Pick the modules (speech-to-text, LLM, voice, or just the LLM) and the models. No provider account. Inbuilt keys cover OpenAI and Deepgram today. | Bot price + $0.25 agent infrastructure + the per-hour price of the models you pick. The dashboard agent editor shows the rate before the bot runs. |
| b. Your own provider keys | Save your keys (OpenAI, Anthropic, Deepgram, ElevenLabs and others) and pick any supported model. | Bot price + $0.25 agent infrastructure. Your provider bills you for model usage. |
| c. Your own agent | Your agent (Claude Code, Codex, Hermes, OpenClaw and others) runs on your computer and writes what the voice agent says, through `model.provider: "local_agent"` and the [MeetStream Agent Kit](https://github.com/meetstream-ai/agent-kit). | Bot price + $0.25 agent infrastructure. MeetStream charges no model tokens for your agent's replies. |

- The bot price is $0.35 per recording hour ($0.25 at volume): https://meetstream.ai/pricing.
- Inbuilt keys are not on for every account yet. If the API answers "MeetStream-managed keys are not available for … Use your own key.", use your own keys. The model does not retry in a loop.
- Setup c is never the default. Meeting speech reaches an agent that has access to your computer and terminal (prompt injection). Your computer, your agent and the bridge must stay on for the whole call. Use the lowest access level. Steps: [MIA Screen Sharing and Custom Agents](https://docs.meetstream.ai/guides/features/mia-screen-sharing-and-custom-agents).

---

## What makes this different from just reading the docs

Every tool description and the `webhook_events_guide` bake in **live-verified ground truth**, confirmed against real production bot runs, that the public API docs currently get wrong:

- **Every delivery carries `event`**, and most also carry `bot_event` with the specific name. Read `event` to route, `bot_event` for detail.
- **Terminals are two-layer**: every ending arrives once with `event: "bot.stopped"`, and `bot_event` says why (`bot.stopped`, `bot.kicked`, `bot.notallowed`, `bot.denied`, `bot.failed`). Lobby timeouts, denials and failures carry `status_code: 500`. Branch on `bot_event`, not `bot_status` - a kick and a clean exit both report `Stopped`.
- **Streaming-only transcription providers** (`deepgram_streaming`, `assemblyai_streaming`, `jigsawstack_streaming`, `meetstream_streaming`, `meeting_captions`) produce no post-call transcript, so `transcription.processed` never fires. `bot.done` still does. A post-call transcript fetch for them returns `202` indefinitely, so cap your polling.
- **`transcript_id` is never in a webhook payload** - `get_transcript` resolves it for you automatically instead of making the model guess.
- Safe defaults everywhere: `automatic_leave` timeouts on every `create_bot` call, and `recording_permission_denied_timeout` floored at 60 (the API rejects lower values with a 400).
- **Errors keep the API's reason.** A tool error shows the API's `message` text. When the API asks you to wait (for example `429` when a prepaid account creates many bots in a short time), the error ends with `(retry after N s)`.

## How it works

The server holds **no API key of its own**: each request (or SSE session) carries the caller's key or an OAuth token that wraps it. A request with no credentials gets a `401` with setup instructions and a `WWW-Authenticate` header pointing OAuth clients at the sign-in flow. Streamable HTTP is stateless, so the service scales horizontally with no shared state; OAuth tokens are encrypted, self-contained values, so there is no token database either.

MeetStream runs the server; it is written in Go and its source is not published. This repo holds the user docs and the [MCP Registry](https://registry.modelcontextprotocol.io) entry (`server.json`). To report a problem, open an issue here.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Sign-in page says the link has expired | Sign-in links last 15 minutes. Start the connection again from your client |
| `401` from `mcp.meetstream.ai` | Sign in through your client (OAuth), or send `Authorization: Bearer <key>` / `X-MeetStream-Api-Key` - the server has no key of its own |
| Tool call returns a 401 from the API | The key is invalid or revoked (for OAuth connections, the key named after the app was deleted). Reconnect, or generate a new key at [app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys) |
| `get_transcript` returns `ready: false` | The meeting hasn't finished processing yet, or (for streaming providers) there is no post-call transcript - check `get_bot_status` first |
| Calendar tools return empty/errors | No calendar is connected yet - connect one via `meetstream calendar connect` in the [CLI](https://github.com/meetstream-ai/meetstream-cli) first |
| Your client only supports the old SSE transport | Use `https://mcp.meetstream.ai/sse` |

---

## Latest docs

- Hosted MCP server: https://docs.meetstream.ai/build-with-ai/meetstream-mcp-server
- Three flows (integration paths): https://docs.meetstream.ai/guides/introduction/integration-paths
- Bridge server (agent on your own stack): https://docs.meetstream.ai/guides/websockets/bridge-server-architecture
- First bot: https://docs.meetstream.ai/guides/get-started/create-your-first-bot
- Voice agent pricing and keys: https://docs.meetstream.ai/guides/mia/pricing-and-keys
- What is MIA: https://docs.meetstream.ai/guides/mia/what-is-mia

For the latest details, fetch the page with `.md` appended, read https://docs.meetstream.ai/llms.txt, or connect the docs MCP server at https://docs.meetstream.ai/_mcp/server. Where this README and the docs differ, the docs are correct.

Prefer a terminal? See the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli). Docs: [docs.meetstream.ai](https://docs.meetstream.ai) · API spec: [openapi.json](https://docs.meetstream.ai/openapi.json) · Migrating from Recall.ai: [@meetstream/migrate](https://github.com/meetstream-ai/recall-meetstream-migration-kit)

## Privacy and telemetry

The hosted server records anonymous counts of which tools are called and whether each call succeeded. It never records your API key, OAuth tokens, meeting URLs, transcripts, or any other content. Server logs contain the request path only, never the query string.

MIT © MeetStream.ai
