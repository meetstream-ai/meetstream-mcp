// MeetStream MCP server — remote Streamable HTTP transport.
// Multi-tenant by design: this process may serve many different MeetStream accounts at once,
// so there is NO shared server-side API key. Each HTTP request supplies its own MeetStream API
// key via the Authorization header (or X-MeetStream-Api-Key), and a fresh MeetStreamClient +
// McpServer instance is built per request — exactly the SDK's documented "stateless mode" pattern.
import { createMcpExpressApp } from '@modelcontextprotocol/sdk/server/express.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { createServer } from './server.js';

const PORT = Number(process.env.PORT || 8080);
const HOST = process.env.HOST || '0.0.0.0';
// Optional server-side fallback key — only useful for a private/single-tenant deployment.
// A public multi-tenant instance should leave this unset and require per-request auth.
const FALLBACK_API_KEY = process.env.MEETSTREAM_API_KEY;

function extractApiKey(req) {
  const auth = req.headers['authorization'];
  if (auth?.toLowerCase().startsWith('bearer ')) return auth.slice(7).trim();
  const custom = req.headers['x-meetstream-api-key'];
  if (custom) return Array.isArray(custom) ? custom[0] : custom;
  return FALLBACK_API_KEY;
}

function unauthorized(res) {
  res.status(401).json({
    jsonrpc: '2.0',
    error: {
      code: -32001,
      message: 'Missing MeetStream API key. Send it as "Authorization: Bearer <key>" or ' +
        '"X-MeetStream-Api-Key: <key>". Create one at https://app.meetstream.ai/api-keys',
    },
    id: null,
  });
}

const app = createMcpExpressApp({ host: HOST, allowedHosts: process.env.MCP_ALLOWED_HOSTS?.split(',') });

app.get('/', (_req, res) => {
  res.json({ name: 'meetstream-mcp', status: 'ok', mcp_endpoint: '/mcp', docs: 'https://github.com/meetstream-ai/meetstream-mcp' });
});
app.get('/health', (_req, res) => res.json({ status: 'ok' }));

app.post('/mcp', async (req, res) => {
  const apiKey = extractApiKey(req);
  if (!apiKey) return unauthorized(res);

  const server = createServer({ apiKey });
  try {
    const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined }); // stateless: one transport per request
    await server.connect(transport);
    await transport.handleRequest(req, res, req.body);
    res.on('close', () => { transport.close(); server.close(); });
  } catch (err) {
    console.error('MCP request error:', err);
    if (!res.headersSent) {
      res.status(500).json({ jsonrpc: '2.0', error: { code: -32603, message: 'Internal server error' }, id: null });
    }
  }
});

// Streamable HTTP is POST-only in stateless mode — no server-push stream or session to resume.
app.get('/mcp', (_req, res) => {
  res.status(405).json({ jsonrpc: '2.0', error: { code: -32000, message: 'Method not allowed. This server runs stateless — use POST.' }, id: null });
});
app.delete('/mcp', (_req, res) => {
  res.status(405).json({ jsonrpc: '2.0', error: { code: -32000, message: 'Method not allowed.' }, id: null });
});

export function startHttpServer() {
  return app.listen(PORT, HOST, () => {
    console.log(`meetstream-mcp (Streamable HTTP) listening on http://${HOST}:${PORT}/mcp`);
  });
}

export { app };
