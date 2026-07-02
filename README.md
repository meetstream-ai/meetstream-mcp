# MeetStream MCP Server

**Give Claude (or any MCP client) direct access to the MeetStream meeting-bot API.** Create bots that join Zoom, Google Meet, and Microsoft Teams meetings; fetch transcripts and AI summaries; send messages into live meetings — all as MCP tools.

## Install

**Claude Code**
```bash
claude mcp add meetstream --env MEETSTREAM_API_KEY=ms_XXXX -- npx -y @meetstream/mcp
```

**Claude Desktop** (`claude_desktop_config.json`)
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

Get a key at [app.meetstream.ai/api-keys](https://app.meetstream.ai/api-keys).

## Then just ask

> "Join my standup at https://meet.google.com/abc-defg-hij, record it with Deepgram transcription, and give me the transcript when it's done."

> "List my bots from today and summarize the 3pm customer call."

> "Send 'we'll follow up by email' into the meeting the bot is in."

## Tools (19)

| Tool | Purpose |
|------|---------|
| `create_bot` | Send/schedule a bot — transcription provider, callbacks, per-participant streams, MIA agent, idempotency |
| `list_bots` · `get_bot_status` · `get_bot_detail` | Inspect bots (detail is the canonical `transcript_id` source) |
| `get_transcript` | Transcript by `bot_id` — resolves `transcript_id` automatically, optional `wait` polling |
| `list_transcriptions` · `transcribe_audio` | List runs / re-transcribe with another provider |
| `get_bot_summary` | MeetStream's built-in AI meeting summary |
| `get_media_urls` | Presigned audio/video/per-participant-stream/screenshot URLs |
| `get_participants` · `get_chats` · `get_speaker_timeline` | Meeting data |
| `send_chat_message` · `send_image` | Interact with a live meeting |
| `remove_bot` | Leave the meeting (keeps data) |
| `delete_bot_data` | Permanent deletion (requires `confirm: true`) |
| `list_calendar_events` · `schedule_calendar_bot` | Google Calendar auto-join |
| `webhook_events_guide` | Live-verified webhook reference — read before writing any handler |

## Design notes

- **Live-verified ground truth baked in**: webhook events arrive under the `event` key; `bot.stopped` is two-layer (`bot_status` says why); streaming-only providers end at `audio.processed`; `transcript_id` is never in webhooks (tools resolve it via `/detail`).
- Safe defaults on `create_bot` (`automatic_leave` timeouts, `recording_permission_denied_timeout` floor of 60).
- Destructive ops are gated (`delete_bot_data` requires `confirm: true`) and annotated for MCP clients.
- `Idempotency-Key` support — retried `create_bot` returns the original bot (HTTP 507), never a duplicate.

## Configuration

| Env var | Purpose |
|---------|---------|
| `MEETSTREAM_API_KEY` | required — your API key |
| `MEETSTREAM_API_URL` | optional base-URL override (default `https://api.meetstream.ai/api/v1`) |

## Development

```bash
npm install
npm test        # spawns the real stdio server and speaks JSON-RPC to it
```

Prefer a terminal? See the [MeetStream CLI](https://github.com/meetstream-ai/meetstream-cli). Docs: [docs.meetstream.ai](https://docs.meetstream.ai) · Spec: [openapi.json](https://docs.meetstream.ai/openapi.json)

MIT © MeetStream.ai
