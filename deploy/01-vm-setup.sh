#!/usr/bin/env bash
# Runs ON the fresh VM (via SSH) to install Docker, nginx, certbot, pull the image, and serve
# mcp.meetstream.ai with TLS. Idempotent — safe to re-run.
#
# Usage (from your laptop):
#   gcloud compute scp deploy/01-vm-setup.sh deploy/nginx-mcp.conf mcp-server:~/ --zone=us-central1-a --project=meetstream-mcp-prod
#   gcloud compute ssh mcp-server --zone=us-central1-a --project=meetstream-mcp-prod --command="chmod +x ~/01-vm-setup.sh && ~/01-vm-setup.sh"
#
# Prereq: the mcp.meetstream.ai DNS A record must already point at this VM's IP
# before running (certbot's HTTP-01 challenge needs it resolvable).
set -euo pipefail

DOMAIN="${MCP_DOMAIN:-mcp.meetstream.ai}"
IMAGE="${MCP_IMAGE:-ghcr.io/meetstream-ai/meetstream-mcp:latest}"  # or build locally, see below
CONTAINER_PORT=8080

echo "== 1. Install Docker (if missing) =="
if ! command -v docker >/dev/null; then
  curl -fsSL https://get.docker.com | sudo sh
  sudo usermod -aG docker "$USER"
fi

echo "== 2. Install nginx + certbot =="
sudo apt-get update -y
sudo apt-get install -y nginx certbot python3-certbot-nginx

echo "== 3. Build & run the MCP server container =="
# If not using a registry image, build directly from the repo instead:
#   git clone https://github.com/meetstream-ai/meetstream-mcp.git ~/meetstream-mcp
#   sudo docker build -t meetstream-mcp:local ~/meetstream-mcp
#   IMAGE=meetstream-mcp:local
sudo docker rm -f meetstream-mcp 2>/dev/null || true
sudo docker run -d --name meetstream-mcp \
  --restart=always \
  -p "127.0.0.1:${CONTAINER_PORT}:8080" \
  "$IMAGE"

echo "== 4. nginx reverse proxy (HTTP first, so certbot's challenge can pass) =="
sudo cp ~/nginx-mcp.conf "/etc/nginx/sites-available/${DOMAIN}"
sudo ln -sf "/etc/nginx/sites-available/${DOMAIN}" "/etc/nginx/sites-enabled/${DOMAIN}"
sudo nginx -t && sudo systemctl reload nginx

echo "== 5. TLS via Let's Encrypt (auto-configures nginx + auto-renews) =="
sudo certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m sidhdharth@meetstream.ai --redirect

echo ""
echo "Done. https://${DOMAIN}/health should now return {\"status\":\"ok\"}"
echo "Verify: curl https://${DOMAIN}/health"
