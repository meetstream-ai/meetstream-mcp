#!/usr/bin/env bash
# Creates a FRESH, isolated GCP project + VM for the MeetStream remote MCP server.
# Deliberately separate from meetstream-n8n / meetstream-firecrawl / meetstram-blog —
# no shared disk, no shared blast radius with other production services.
#
# Prereqs: `gcloud auth login --update-adc` must have been run interactively first
# (this session's auth token was RAPT-expired and needs a one-time browser re-login).
#
# Usage: ./00-create-infra.sh
set -euo pipefail

PROJECT_ID="${MCP_PROJECT_ID:-meetstream-mcp-prod}"
BILLING_ACCOUNT="${MCP_BILLING_ACCOUNT:-01FD5E-E5BF3B-E3EB75}"   # Singapore Master Billing (existing MeetStream account)
ZONE="${MCP_ZONE:-us-central1-a}"
VM_NAME="${MCP_VM_NAME:-mcp-server}"
MACHINE_TYPE="${MCP_MACHINE_TYPE:-e2-small}"                     # 2 vCPU burst / 2GB — plenty for this workload

echo "== 1. Create project =="
gcloud projects create "$PROJECT_ID" --name="MeetStream MCP Server" || echo "(project may already exist — continuing)"

echo "== 2. Link billing =="
gcloud billing projects link "$PROJECT_ID" --billing-account="$BILLING_ACCOUNT"

echo "== 3. Enable required APIs =="
gcloud services enable compute.googleapis.com --project="$PROJECT_ID"

echo "== 4. Firewall: allow HTTP/HTTPS + SSH only =="
gcloud compute firewall-rules create allow-http-https \
  --project="$PROJECT_ID" --network=default \
  --allow=tcp:80,tcp:443,tcp:22 --source-ranges=0.0.0.0/0 \
  --description="Public HTTP/HTTPS for mcp.meetstream.ai + SSH for admin" || echo "(rule may already exist)"

echo "== 5. Create the VM (Container-Optimized OS, so Docker is preinstalled) =="
gcloud compute instances create "$VM_NAME" \
  --project="$PROJECT_ID" --zone="$ZONE" --machine-type="$MACHINE_TYPE" \
  --image-family=debian-12 --image-project=debian-cloud \
  --boot-disk-size=20GB --boot-disk-type=pd-balanced \
  --tags=http-server,https-server

echo ""
echo "== Done. VM external IP: =="
gcloud compute instances describe "$VM_NAME" --project="$PROJECT_ID" --zone="$ZONE" \
  --format="value(networkInterfaces[0].accessConfigs[0].natIP)"
echo ""
echo "Next: point mcp.meetstream.ai (Route 53 A record) at that IP, then run 01-vm-setup.sh"
echo "SSH:  gcloud compute ssh $VM_NAME --project=$PROJECT_ID --zone=$ZONE"
