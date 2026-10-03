# MeetStream MCP server (Go) - remote Streamable HTTP transport (mcp.meetstream.ai).
# Build from the repo root:  docker build -f go/Dockerfile -t meetstream-mcp-go go
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.4.0
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/meetstream-mcp ./cmd/meetstream-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/meetstream-mcp /meetstream-mcp
ENV PORT=8080 HOST=0.0.0.0
EXPOSE 8080
USER nonroot:nonroot
# No MEETSTREAM_API_KEY: multi-tenant, each request carries its own key or OAuth token.
ENTRYPOINT ["/meetstream-mcp"]
CMD ["http"]
