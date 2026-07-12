#!/usr/bin/env node
// Remote entry point — Streamable HTTP transport, for hosting (e.g. mcp.meetstream.ai).
// For local/desktop use with a single MEETSTREAM_API_KEY, use bin/meetstream-mcp.js (stdio) instead.
import { startHttpServer } from '../src/http-server.js';

startHttpServer();

process.on('SIGTERM', () => process.exit(0));
process.on('SIGINT', () => process.exit(0));
