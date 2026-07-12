# Deploying the remote MCP server (`mcp.meetstream.ai`)

This deploys `@meetstream/mcp` as a **standalone, isolated** service — its own GCP project, its
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
domain — no shared credential, no cross-tenant risk.

## One-time prerequisites

1. **GCP auth** (this machine's `sidhdharth@meetstream.ai` token needs a one-time interactive
   refresh — cannot be done from a non-interactive session):
   ```bash
   gcloud auth login --update-adc
   ```
2. **AWS Route 53 access** — an IAM credential with `route53:ChangeResourceRecordSets` /
   `route53:ListHostedZones` on the `meetstream.ai` hosted zone. Neither credential set present
   on this machine as of this writing has that permission (`meetstream-ro` and `ms-bots` are both
   scoped to other things).

## Deploy sequence

```bash
# 1. Create the isolated project + VM (run from your laptop, after gcloud auth login --update-adc)
./00-create-infra.sh
# → prints the VM's external IP

# 2. Point DNS at it (Route 53) — fill in the IP from step 1
#    Either via AWS CLI:
aws route53 change-resource-record-sets \
  --hosted-zone-id <MEETSTREAM_AI_ZONE_ID> \
  --change-batch file://route53-change-batch.json   # edit the IP in this file first
#    ...or via the Route 53 console: add an A record, mcp.meetstream.ai -> <VM IP>, TTL 300.

# 3. Wait for DNS propagation (usually <5 min with TTL 300), then confirm:
dig +short mcp.meetstream.ai

# 4. Copy setup files onto the VM and run them
gcloud compute scp 01-vm-setup.sh nginx-mcp.conf mcp-server:~/ \
  --zone=us-central1-a --project=meetstream-mcp-prod
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp-prod \
  --command="chmod +x ~/01-vm-setup.sh && ~/01-vm-setup.sh"

# 5. Verify
curl https://mcp.meetstream.ai/health
# {"status":"ok"}
```

The GitHub Actions workflow (`.github/workflows/docker-publish.yml`) builds and pushes
`ghcr.io/meetstream-ai/meetstream-mcp:latest` on every push to `main` — `01-vm-setup.sh` pulls
that image, so redeploys after a code change are just:
```bash
gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp-prod \
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

No `npx`, no local Node install, no per-machine setup — the client just needs its own MeetStream
API key.

## Cost estimate

`e2-small` (2 vCPU burst, 2GB RAM), 20GB pd-balanced disk, us-central1-a — roughly **$13–15/mo**,
in line with the existing n8n/blog VMs on the same billing account.
