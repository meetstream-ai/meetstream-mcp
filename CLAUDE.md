# CLAUDE.md

Claude Code does not read `AGENTS.md` natively, so this file imports it. Everything in it applies here.

@AGENTS.md

## Claude Code specifics

Install this server:

```sh
claude mcp add --transport http meetstream https://mcp.meetstream.ai/mcp \
  --header "Authorization: Bearer $MEETSTREAM_API_KEY"
```

Or install the plugin, which bundles the server plus skills for joining meetings, summarizing calls, MIA voice agents, webhooks, streaming, calendar automation and troubleshooting:

```sh
/plugin marketplace add meetstream-ai/claude-plugin
```

Confirm it is live with `/mcp`, then ask for `list_bots` - a clean response means the key and transport are both good.

## Keep these two files in sync

`CLAUDE.md` imports `AGENTS.md` rather than duplicating it, so edit **`AGENTS.md`** and the change lands in both. Only Claude-Code-specific instructions belong in this file.
