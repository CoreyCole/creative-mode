---
date: 2026-02-14T11:55:50-08:00
researcher: CoreyCole
git_commit: d88eed7
branch: main
repository: creative-mode
topic: "AWS Marketing Site HTTPS + API Gateway Setup"
tags: [infrastructure, aws, api-gateway, route53, acm, https, marketing-site]
status: in_progress
last_updated: 2026-02-14
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: AWS Marketing Site Setup

## Task(s)

**Set up the marketing site on AWS with HTTPS** — Task #1 from the session's TODO list. Status: **in progress**.

### Completed
1. **Cleaned up AWS CLI credentials** — Removed stale Blue Canoe Learning profiles (default, ciremi, ciremi-prod, ciremi-amplify-prod) from `~/.aws/config` and `~/.aws/credentials`. Both `default` and `creative-mode` profiles now point to account `975050104386` (Corey's personal AWS account).
2. **Deleted stale ACM cert** — Removed a `creative-mode.com` (wrong domain) PENDING_VALIDATION cert from us-west-2.
3. **Requested and validated ACM cert** — New cert for `creative-mode.ai` + `*.creative-mode.ai` in us-west-2. Validated via DNS (CNAME added to Route 53). Status: **ISSUED**.
   - Cert ARN: `arn:aws:acm:us-west-2:975050104386:certificate/c4969c1b-4354-43fc-b4a8-8e6369f4f625`
4. **Created HTTP API Gateway** — Regional HTTP API (`f1jx4kmn4h`) in us-west-2 with:
   - `ANY /{proxy+}` route → HTTP_PROXY integration to `http://52.32.199.228/{proxy}` (integration ID: `pfc6b1f`)
   - `$default` route → HTTP_PROXY integration to `http://52.32.199.228/` (integration ID: `m6s6lxb`)
   - Auto-deploy stage (`$default`)
5. **Custom domain configured** — `creative-mode.ai` mapped to API Gateway with TLS 1.2.
   - API Gateway domain: `d-yz9mxo0pj4.execute-api.us-west-2.amazonaws.com`
   - Hosted zone for API GW: `Z2OJLYMUO9EFXC`
6. **Route 53 DNS configured** — A record alias from `creative-mode.ai` → API Gateway. DNS propagated and verified.
7. **HTTPS verified** — `curl https://creative-mode.ai/` returns HTTP 503 (expected — no web server on EC2 yet).

### Not yet done
- Clone creative-mode repo on EC2
- Install and configure nginx on EC2 to serve the marketing site
- Secure the EC2 server (following `scripts/vps-bootstrap.sh` patterns)
- Deploy the marketing site content

## Critical References
- `scripts/vps-bootstrap.sh` — VPS bootstrap script with security hardening (UFW, DOCKER-USER iptables, fail2ban, SSH lockdown, Tailscale). Should be adapted for the EC2 instance.
- `site/` directory — The marketing site code (Go + templ + Datastar)

## Recent changes
- `~/.aws/config` — Removed Blue Canoe profiles, kept only default + creative-mode (us-west-2)
- `~/.aws/credentials` — Removed Blue Canoe credentials, both profiles now use creative-mode account keys

## Learnings
- **AWS account**: Creative Mode uses account `975050104386` (user `corey`), accessed via `AWS_PROFILE=creative-mode` or the default profile.
- **Region**: Everything is in **us-west-2** — EC2, ACM cert, API Gateway. Regional API Gateway was chosen (not edge-optimized) so the cert can stay in us-west-2.
- **API Gateway type**: HTTP API (v2), not REST API (v1). Cheaper and simpler for proxying to EC2.
- **Two integrations needed**: The `$default` route can't use `{proxy}` path variable, so a separate integration without the path variable is needed for the root path.
- **EC2 has no web server**: `52.32.199.228` doesn't respond to HTTP requests yet. Port 80 may not be open in the security group either.

## Artifacts
- ACM Certificate: `arn:aws:acm:us-west-2:975050104386:certificate/c4969c1b-4354-43fc-b4a8-8e6369f4f625`
- API Gateway: `f1jx4kmn4h` in us-west-2
- Route 53 Hosted Zone: `Z05191912AIR8N0TYX8IU` (creative-mode.ai)
- EC2 Instance: `i-0ec415fe12d69de3f` at `52.32.199.228` (t2.micro, us-west-2)

## Action Items & Next Steps

1. **SSH into the EC2** — Determine access method (key pair name, security group). May need to check AWS console for the key pair associated with the instance.
2. **Open port 80 on EC2 security group** — API Gateway sends HTTP to the EC2's public IP on port 80. Verify the security group allows inbound HTTP.
3. **Clone creative-mode repo on EC2** — `git clone https://github.com/CoreyCole/creative-mode.git`
4. **Install nginx** — Set up as reverse proxy to the marketing site (likely the `site/` Go app running on a local port).
5. **Run the marketing site** — The `site/` directory has its own `docker-compose.yml` and `justfile`. Determine how to run it on the EC2.
6. **Secure the EC2** — Adapt `scripts/vps-bootstrap.sh` for the EC2:
   - Deploy user, UFW firewall, fail2ban, SSH hardening
   - Docker daemon config, DOCKER-USER iptables rules
   - Consider whether Tailscale is needed on this instance (it's public-facing via API Gateway, unlike the harness VPS)
7. **Set up `www.creative-mode.ai`** — Consider adding a www subdomain CNAME or redirect.

## Other Notes

- The TODO list from this session has 9 tasks total. Task #1 (this one) is in progress. Tasks #2 (Mac VM + Tailscale) and #3 (Discord bot tokens) are independent and can be done in parallel. Tasks #4-#9 are the World Mayors master plan phases (sequential).
- The `site/` directory appears to be a separate Go marketing site (not the harness). It has its own `docker-compose.yml`, `go.mod`, `main.go`, templ templates, and static assets.
- The existing `datastar-ui.com` cert and hosted zone in this account suggest a similar setup was done before — could be a useful reference pattern.
- EC2 instance is named "coreycc.com" — may have been originally set up for a different purpose.
