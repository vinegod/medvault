# Serving MedVault over HTTPS

Browsers enable the service worker, `crypto.randomUUID`, `crypto.subtle`
and the clipboard only on HTTPS or `localhost`. Over plain HTTP on a LAN
address, adding medicines, photo upload, offline mode and installing the
PWA do not work. Pick one of the setups below.

| Setup | Reachable from | Certificate | Needs |
|-------|----------------|-------------|-------|
| [Tailscale](#tailscale) | Your devices, anywhere | Trusted, automatic | Tailscale app on every device |
| [Caddy + public domain](#caddy-with-a-public-domain) | The internet | Let's Encrypt | Domain, ports 80/443 forwarded |
| [Caddy, LAN only](#caddy-lan-only) | Home network | Caddy's own CA | Root certificate installed on every device |

Use Tailscale or Caddy, not both on the same host.

## Tailscale

Tailscale gives every device a private network and a trusted
`*.ts.net` certificate. Nothing is exposed to the internet, and phones
reach MedVault away from home.

1. Install Tailscale on the server and on every phone/laptop, and sign in
   with the same account. On the server:

   ```bash
   curl -fsSL https://tailscale.com/install.sh | sh
   sudo tailscale up
   ```

2. In the Tailscale admin console, open **DNS** and enable **MagicDNS**
   and **HTTPS Certificates**.

3. Publish MedVault on localhost only. In `.env`:

   ```bash
   PORT=8090            # any free port
   BIND_ADDR=127.0.0.1
   ```

   ```bash
   docker compose up -d --build
   ```

4. Serve it over HTTPS on the tailnet:

   ```bash
   sudo tailscale serve --bg http://127.0.0.1:8090
   tailscale serve status    # prints the https://<host>.<tailnet>.ts.net URL
   ```

5. On a phone with Tailscale connected, open that URL and use
   **Add to Home Screen**.

To stop serving: `sudo tailscale serve reset`.

Do not enable Tailscale Funnel unless you want MedVault on the public
internet. All requests reach MedVault from one local address, so the
per-IP login and register limits are shared by the tailnet; that is
fine for a household.

## Caddy

`docker-compose.caddy.yml` adds a Caddy container in front of MedVault.
MedVault's own port is then not published on the host, so `PORT` can be
anything. Enable it in `.env`:

```bash
COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml
```

`docker compose up -d --build` then starts both containers.

| Variable | Default | Description |
|----------|---------|-------------|
| `DOMAIN` | required | Host name or IP the site is served on. |
| `CADDY_TLS` | `internal` | `internal` for Caddy's own CA, or an email address for Let's Encrypt. |
| `HTTP_PORT` | `80` | Host port for HTTP, which redirects to HTTPS. |
| `HTTPS_PORT` | `443` | Host port for HTTPS. |
| `CADDY_SUBNET` | `172.30.250.0/24` | Subnet of the compose network. Change it only if it clashes with an existing network. |
| `CADDY_IP` | `172.30.250.10` | Caddy's address in that subnet. MedVault trusts `X-Forwarded-For` only from this address. |

Requires Docker Compose 2.24.4 or later (for `ports: !reset`). After
changing `CADDY_SUBNET`, run `docker compose down` once so the network
is recreated.

If Caddy sees the Docker gateway address instead of the real client
(IPv6 or Docker's userland proxy), all clients share one set of per-IP
rate limits.

### Caddy with a public domain

1. Point a DNS `A` record (e.g. `meds.example.com`) at your public IP.
2. Forward ports 80 and 443 on the router to the server. Let's Encrypt
   validates on these ports, so keep `HTTP_PORT` and `HTTPS_PORT` at their
   defaults.
3. In `.env`:

   ```bash
   COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml
   DOMAIN=meds.example.com
   CADDY_TLS=you@example.com
   REGISTRATION_MODE=invite
   ```

4. `docker compose up -d --build`, then open `https://meds.example.com`.

### Caddy, LAN only

1. In `.env`, use the server's LAN IP or a local name, and any free ports
   if 80/443 are taken:

   ```bash
   COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml
   DOMAIN=192.168.1.10
   CADDY_TLS=internal
   HTTP_PORT=8081
   HTTPS_PORT=8443
   ```

2. `docker compose up -d --build`.

3. Copy Caddy's root certificate and install it on every device:

   ```bash
   docker compose cp caddy:/data/caddy/pki/authorities/local/root.crt ./medvault-root.crt
   ```

   - Android: Settings → Security → Encryption & credentials → Install a
     certificate → CA certificate.
   - iOS: open the file, install the profile, then enable it under
     Settings → General → About → Certificate Trust Settings.
   - Desktop: import it into the system or browser trust store.

4. Open `https://192.168.1.10:8443`.

The root certificate stays valid while the `caddy-data` volume exists; do
not delete that volume or every device needs the new certificate.
