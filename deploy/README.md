# ccam gateway — VPS deployment

The gateway is the data plane: it holds the shared Claude subscriptions and
proxies each team member's Claude Code to Anthropic, so many people use one
account at once and none of them ever holds its credential.

- Host: a plain Linux box (Ubuntu). Binary at `/opt/ccam-gateway/ccam-gateway`,
  config at `/opt/ccam-gateway/env` (0600).
- Fronted by nginx + Let's Encrypt at `https://ccam-gw.<ip>.sslip.io`
  (`sslip.io` resolves any label to the IP, so no DNS to manage).
- `deploy/ccam-gateway.service` is the systemd unit; `deploy/nginx-ccam-gw.conf`
  the nginx site (certbot adds the 443 block).
- CI (`deploy-gateway` job) cross-compiles for linux/amd64 and ships the binary
  on every push to main, then restarts the service. Secrets: `VPS_SSH_KEY`,
  `VPS_HOST`; gated on the `GATEWAY_DEPLOY` repo variable.
