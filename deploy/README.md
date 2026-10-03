# Deploying the remote MCP server (`mcp.meetstream.ai`)

This deploys `@meetstream/mcp` as a **standalone, isolated** service - its own GCP project, its
own VM, its own disk. Deliberately **not** on the shared `meetstream-n8n` VM (or any other
existing MeetStream service) to keep blast radius contained.

## Architecture

```
Internet ──HTTPS──► nginx (Let's Encrypt TLS) ──► Docker container :8080 ──► MeetStream API
mcp.meetstream.ai         on the VM                  meetstream-mcp            (per-request
   (Route 53 A record)                              (this repo, HTTP           Authorization
                                                       transport)               header → real
                                                                                MeetStream key)
```

Multi-tenant by design: the server holds **no MeetStream API key of its own**. Every caller
supplies their own key per request via `Authorization: Bearer <key>` or `X-MeetStream-Api-Key`
(see `src/http-server.js`). This is what makes it safe to expose publicly under the MeetStream
domain - no shared credential, no cross-tenant risk.

## One-time prerequisites

1. **GCP auth** (this machine's `sidhdharth@meetstream.ai` token needs a one-time interactive
   refresh - cannot be done from a non-interactive session):
   ```bash
   gcloud auth login --update-adc
   ```
2. **AWS Route 53 access** - an IAM credential with `route53:ChangeResourceRecordSets` /
   `route53:ListHostedZones` on the `meetstream.ai` hosted zone. Neither credential set present
   on this machine as of this writing has that permission (`meetstream-ro` and `ms-bots` are both
   scoped to other things).

## Deploy sequence

```bash
# 1. Create the isolated project + VM (run from your laptop, after gcloud auth login --update-adc)
./00-create-infra.sh
# → prints the VM's external IP

# 2. Point DNS at it (Route 53) - fill in the IP from step 1
#    Either via AWS CLI:
aws route53 change-resource-record-sets \
  --hosted-zone-id <MEETSTREAM_AI_ZONE_ID> \
  --change-batch file://route53-change-batch.json   # edit the IP in this file first
#    ...or via the Route 53 console: add an A record, mcp.meetstream.ai -> <VM IP>, TTL 300.

# 3. Wait for DNS propagation (usually <5 min with TTL 300), then confirm:
dig +short mcp.meetstream.ai

# 4. Copy setup files onto the VM and run them
gcloud compute scp 01-vm-setup.sh nginx-mcp.conf mcp-server:~/ \
  --zone=us-central1-a --project=meetstream-mcp
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp \
  --command="chmod +x ~/01-vm-setup.sh && ~/01-vm-setup.sh"

# 5. Verify
curl https://mcp.meetstream.ai/health
# {"status":"ok"}
```

The GitHub Actions workflow (`.github/workflows/docker-publish.yml`) builds and pushes
`ghcr.io/meetstream-ai/meetstream-mcp:latest` on every push to `main` - `01-vm-setup.sh` pulls
that image, so redeploys after a code change are just:
```bash
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp \
  --command="sudo docker pull ghcr.io/meetstream-ai/meetstream-mcp:latest && sudo docker rm -f meetstream-mcp && sudo docker run -d --name meetstream-mcp --restart=always -p 127.0.0.1:8080:8080 ghcr.io/meetstream-ai/meetstream-mcp:latest"
```

## Using the deployed server

Once live, any MCP client that supports remote (Streamable HTTP) servers can add it directly by URL:

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

No `npx`, no local Node install, no per-machine setup - the client just needs its own MeetStream
API key.

## Cost estimate

`e2-small` (2 vCPU burst, 2GB RAM), 20GB pd-balanced disk, us-central1-a.

## Redeploying a new version

The running container is built **on the VM** from this repo, not pulled from a
registry. The CI-published image at `ghcr.io/meetstream-ai/meetstream-mcp:latest`
is **private**, so `docker pull` returns `unauthorized` from the VM. Either make
that package public in GitHub's package settings, or keep using the local build
below (which is what production currently runs).

```sh
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp

# on the VM
rm -rf ~/meetstream-mcp
git clone --depth 1 https://github.com/meetstream-ai/meetstream-mcp.git ~/meetstream-mcp
sudo docker build -t meetstream-mcp:vX.Y.Z ~/meetstream-mcp
sudo docker run --rm meetstream-mcp:vX.Y.Z node -e 'console.log(require("/app/package.json").version)'

# swap, keeping the old container for rollback
sudo docker stop meetstream-mcp
sudo docker rename meetstream-mcp meetstream-mcp-rollback
sudo docker run -d --name meetstream-mcp --restart always \
  -p 127.0.0.1:8080:8080 \
  -e NODE_ENV=production -e PORT=8080 -e HOST=0.0.0.0 \
  meetstream-mcp:vX.Y.Z
```

Verify from your laptop before walking away:

```sh
curl -s -X POST https://mcp.meetstream.ai/mcp \
  -H "Authorization: Bearer $MEETSTREAM_API_KEY" \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"v","version":"1"}}}' \
  | grep -o '"version":"[^"]*"'
```

**Rollback:** `sudo docker stop meetstream-mcp && sudo docker rm meetstream-mcp && sudo docker rename meetstream-mcp-rollback meetstream-mcp && sudo docker start meetstream-mcp`

## Keys in logs

Claude's custom connectors can only pass the API key as `?key=`. nginx's default access log records the full request line, query string included, so `nginx-mcp.conf` defines a `mcp_noquery` log format that logs the path only. On an existing VM, apply it by hand, because certbot has already rewritten the live site file:

1. Add the `log_format mcp_noquery ...` line above the `server {` blocks in `/etc/nginx/sites-available/mcp.meetstream.ai`, and `access_log /var/log/nginx/mcp.access.log mcp_noquery;` inside each `server` block.
2. `sudo nginx -t && sudo systemctl reload nginx`
3. Purge the logs written before the change, which contain keys: `sudo truncate -s 0 /var/log/nginx/access.log*` and remove the rotated `.gz` files.

nginx's error log can also quote the request line when an upstream error occurs. Keep it at the default `error` level and rotate it.

## Go server (production since 2026-10-03)

`mcp.meetstream.ai` is served by the Go implementation in [`go/`](../go). The Node container stays
on the VM, stopped from traffic but running, as an instant rollback.

| Container | Image | Port | Role |
|-----------|-------|------|------|
| `meetstream-mcp-go` | `meetstream-mcp:go-v<version>` | 127.0.0.1:8081 | live (nginx upstream) |
| `meetstream-mcp` | `meetstream-mcp:v0.3.3` (Node) | 127.0.0.1:8080 | rollback |

Settings live in `/etc/meetstream-mcp/go.env` (root, 0600): `MCP_OAUTH_MODE`, `MCP_PUBLIC_URL`,
`MCP_OAUTH_SECRET`, and in dashboard mode `MCP_DASHBOARD_GRANT_KEY` + `MEETSTREAM_DASHBOARD_URL`.
The secrets were generated on the VM and are not stored anywhere else. Rotating `MCP_OAUTH_SECRET`:
prepend a new key (`new,old`), restart, and drop the old one after 90 days (the refresh-token lifetime).

Build and deploy (no CI involved; the image is built on the VM):
```bash
git archive HEAD go | gzip > /tmp/go-src.tgz
gcloud compute scp /tmp/go-src.tgz mcp-server:~/go-src.tgz --zone=us-central1-a --project=meetstream-mcp
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp --command='
  rm -rf ~/mcp-go && mkdir ~/mcp-go && tar xzf ~/go-src.tgz -C ~/mcp-go && cd ~/mcp-go/go &&
  sudo docker build -q --build-arg VERSION=0.4.0 -t meetstream-mcp:go-v0.4.0 . &&
  sudo docker rm -f meetstream-mcp-go &&
  sudo docker run -d --name meetstream-mcp-go --restart=always --env-file /etc/meetstream-mcp/go.env \
    -p 127.0.0.1:8081:8080 --memory=256m meetstream-mcp:go-v0.4.0'
```

Rollback to Node (seconds): point nginx back at 8080.
```bash
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp --command='
  T=$(readlink -f /etc/nginx/sites-enabled/mcp) &&
  sudo sed -i "s#proxy_pass http://127.0.0.1:8081;#proxy_pass http://127.0.0.1:8080;#" $T &&
  sudo nginx -t && sudo systemctl reload nginx'
```
A pre-cutover copy of the nginx site is at `/etc/nginx/mcp.conf.bak-node-202610031922`.

### Default nginx log (2026-10-03)

The site file only covers `mcp.meetstream.ai`. Requests that miss it (e.g. the bare IP) fell through to
nginx's default `access_log` in `/etc/nginx/nginx.conf`, which recorded full query strings, so `?key=`
values leaked there from 11 to 19 Sep. That line now uses a path-only `default_noquery` format
(backup: `/etc/nginx/nginx.conf.bak-20261003`). On a fresh VM, apply the same change:
```nginx
log_format default_noquery '$remote_addr - $remote_user [$time_local] "$request_method $uri $server_protocol" $status $body_bytes_sent "$http_referer" "$http_user_agent"';
access_log /var/log/nginx/access.log default_noquery;
```
