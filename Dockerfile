# MeetStream MCP server — remote Streamable HTTP transport (mcp.meetstream.ai)
FROM node:20-slim

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install --omit=dev --no-audit --no-fund

COPY bin ./bin
COPY src ./src

ENV NODE_ENV=production
ENV PORT=8080
ENV HOST=0.0.0.0
EXPOSE 8080

# No default MEETSTREAM_API_KEY — this is a multi-tenant remote server; each request
# supplies its own key via the Authorization header. See README "Remote server" section.
USER node
CMD ["node", "bin/meetstream-mcp-http.js"]
