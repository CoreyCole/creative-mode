# Caddy Reverse Proxy for SSE Support — Implementation Plan

## Overview

AWS API Gateway buffers responses and has a ~30s integration timeout, which breaks long-lived SSE connections like `/status/events`. Replace API Gateway with Caddy on EC2 for automatic TLS (Let's Encrypt) and native streaming support. Only affects `creative-mode.ai` — other sites on the same EC2 keep their API Gateway setups.

## Current State Analysis

- Traffic flow: `Browser -> Route 53 -> API Gateway (TLS/ACM) -> EC2:80 -> site binary`
- Site binary listens on port 80 via `CAP_NET_BIND_SERVICE` (`site/creative-mode-site.service:10`)
- Default port set in `site/main.go:382-384`
- Mayor chat SSE (short-lived, POST) works through API Gateway
- Status events SSE (long-lived, GET, loops forever) does NOT work through API Gateway

## Desired End State

- Traffic flow: `Browser -> Route 53 -> EC2 Elastic IP -> Caddy:443 (TLS) -> localhost:3000`
- Caddy handles TLS via Let's Encrypt, streams SSE natively
- API Gateway and ACM certificate for creative-mode.ai are deleted
- Other sites on the same EC2 are unaffected

### Verification:
- `curl -N http://localhost:3000/status/events` on EC2 streams SSE events
- `https://creative-mode.ai/status` shows live metrics data in browser
- Other sites' API Gateway setups still work

## What We're NOT Doing

- Changing other sites' infrastructure
- Adding nginx or any other proxy layer
- Modifying the SSE implementation itself
- Changing the Docker dev setup (only affects production EC2)

## Implementation Approach

Minimal changes: switch the site's default port to 3000, add a Caddyfile, update the systemd unit, then do infra changes on EC2 and AWS.

## Phase 1: Code Changes (local)

### Changes Required:

#### 1. Default port
**File**: `site/main.go:384`
**Change**: Default port from `"80"` to `"3000"`

#### 2. Systemd unit
**File**: `site/creative-mode-site.service`
**Change**: Remove `AmbientCapabilities=CAP_NET_BIND_SERVICE` (no longer binding port 80)

#### 3. Caddyfile
**File**: `site/Caddyfile` (new)
**Content**:
```
creative-mode.ai {
    reverse_proxy localhost:3000
}
```

#### 4. Environment example
**File**: `site/site.env.example`
**Change**: Update `PORT` comment to show default is 3000

#### 5. Documentation
**File**: `site/CLAUDE.md` (if it exists) and root `CLAUDE.md`
**Change**: Update traffic flow diagram from API Gateway to Caddy

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes

#### Manual Verification:
- [ ] Local Docker dev still works (uses PORT env var, unaffected by default change)

---

## Phase 2: EC2 Setup (on server)

### Steps:

#### 1. Install Caddy (official repo for Ubuntu)
```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

#### 2. Deploy updated code
```bash
cd ~/creative-mode && git pull
cd site && just build && cp site-linux /tmp/creative-mode-site
```

#### 3. Copy configs
```bash
sudo cp ~/creative-mode/site/Caddyfile /etc/caddy/Caddyfile
sudo cp ~/creative-mode/site/creative-mode-site.service /etc/systemd/system/
sudo systemctl daemon-reload
```

#### 4. Open firewall ports
```bash
sudo ufw allow 80/tcp && sudo ufw allow 443/tcp
```

#### 5. Update site.env
Add or change `PORT=3000` in `~/.config/creative-mode/site.env`

#### 6. Restart services
```bash
sudo systemctl restart creative-mode-site
sudo systemctl enable --now caddy
```

### Success Criteria:

#### Manual Verification:
- [ ] `curl -N http://localhost:3000/status/events` streams SSE events
- [ ] `sudo systemctl status caddy` shows active
- [ ] `sudo systemctl status creative-mode-site` shows active

---

## Phase 3: AWS Cleanup (from local machine)

### Steps:

#### 1. Identify resources
```bash
# List HTTP APIs (API Gateway v2)
aws apigatewayv2 get-apis --query 'Items[*].[ApiId,Name]' --output table

# List REST APIs (API Gateway v1)
aws apigateway get-rest-apis --query 'items[*].[id,name]' --output table

# List ACM certificates
aws acm list-certificates --query 'CertificateSummaryList[*].[CertificateArn,DomainName]' --output table
```

#### 2. Update Route 53 — point to EC2 Elastic IP
```bash
# Find hosted zone
aws route53 list-hosted-zones --query 'HostedZones[*].[Id,Name]' --output table

# Get current record (replace ZONE_ID)
aws route53 list-resource-record-sets --hosted-zone-id ZONE_ID \
  --query "ResourceRecordSets[?Name=='creative-mode.ai.']"

# Update A record (replace ZONE_ID and ELASTIC_IP)
aws route53 change-resource-record-sets --hosted-zone-id ZONE_ID --change-batch '{
  "Changes": [{"Action": "UPSERT", "ResourceRecordSet": {
    "Name": "creative-mode.ai", "Type": "A", "TTL": 300,
    "ResourceRecords": [{"Value": "ELASTIC_IP"}]
  }}]
}'
```

#### 3. Delete API Gateway (after DNS propagation, ~5 min)
```bash
# For HTTP API (v2)
aws apigatewayv2 delete-api --api-id API_ID

# For REST API (v1)
aws apigateway delete-rest-api --rest-api-id API_ID
```

#### 4. Delete ACM certificate (optional, after API Gateway deleted)
```bash
aws acm delete-certificate --certificate-arn CERT_ARN
```

### Success Criteria:

#### Manual Verification:
- [ ] `https://creative-mode.ai/status` loads with TLS (Let's Encrypt cert)
- [ ] All metrics populate live via SSE
- [ ] Other sites still work through their API Gateways
- [ ] `dig creative-mode.ai` shows EC2 Elastic IP

## References

- Handoff: `thoughts/CoreyCole/handoffs/general/2026-02-16_03-38-09_status-page-sse-root-cause.md`
- SSE handler: `site/internal/monitor/handler.go:189-261`
- Route registration: `site/main.go:172-178`
- Systemd unit: `site/creative-mode-site.service`
- Deployment topology: root `CLAUDE.md` "Deployment Topology" section
