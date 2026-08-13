#!/usr/bin/env node
// MeetStream MCP server (stdio) — https://github.com/meetstream-ai/meetstream-mcp
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { createServer } from '../src/server.js';
import { setTransport, track, shutdownTelemetry } from '../src/telemetry.js';

setTransport('local');

const server = createServer();
const transport = new StdioServerTransport();
await server.connect(transport);
track('mcp_server_started');
for (const sig of ['SIGINT', 'SIGTERM']) process.on(sig, async () => { await shutdownTelemetry(); process.exit(0); });
// stdio transport: never write logs to stdout (it is the protocol channel)
console.error('meetstream-mcp ready (stdio). Set MEETSTREAM_API_KEY in this process env.');
