# Deploying the Oddvice API (Ubuntu/Debian + systemd + Caddy)

The API is a single Go binary. It listens on `127.0.0.1:8080`; Caddy terminates
HTTPS on your subdomain and reverse-proxies to it.

## 0. DNS
Create an **A record**: `api.yourdomain.com → <VPS_IP>`. Wait for it to resolve
(`dig +short api.yourdomain.com`).

## 1. Install Go + Caddy (once)
```bash
sudo snap install go --classic            # Go 1.22+ required
go version

# Caddy (official apt repo)
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt'  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

## 2. Get the code
```bash
sudo useradd --system --home /opt/oddvice-api --shell /usr/sbin/nologin oddvice
sudo git clone https://github.com/Scroti/oddvice-api.git /opt/oddvice-api
cd /opt/oddvice-api
sudo go build -o bin/server ./cmd/server
```

## 3. Configure secrets
```bash
sudo cp .env.example .env
sudo nano .env
```
Set at least:
```
APP_ENV=production
HOST=127.0.0.1
PORT=8080
CORS_ALLOWED_ORIGINS=https://your-web-domain.com,http://localhost:3000
FOOTBALL_DATA_API_KEY=...     # from your local api/.env
APIFOOTBALL_API_KEY=...       # from your local api/.env
APIFOOTBALL_SEASON=2026
```

## 4. systemd service
```bash
sudo cp deploy/oddvice-api.service /etc/systemd/system/
sudo chown -R oddvice:oddvice /opt/oddvice-api
sudo systemctl daemon-reload
sudo systemctl enable --now oddvice-api
systemctl status oddvice-api          # should be active (running)
journalctl -u oddvice-api -f          # live logs
curl -s http://127.0.0.1:8080/healthz # {"status":"ok",...}
```

## 5. Caddy (HTTPS)
```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo sed -i 's/api.yourdomain.com/<YOUR_SUBDOMAIN>/' /etc/caddy/Caddyfile
sudo systemctl reload caddy
curl -s https://<YOUR_SUBDOMAIN>/healthz   # works over HTTPS
```

## 6. Firewall
```bash
sudo ufw allow OpenSSH
sudo ufw allow 80,443/tcp     # Caddy; 8080 stays internal
sudo ufw enable
```

## 7. Point the apps at it
- Web: `NEXT_PUBLIC_API_URL=https://<YOUR_SUBDOMAIN>`
- Flutter: `--dart-define=API_URL=https://<YOUR_SUBDOMAIN>`
- Add those web origins to `CORS_ALLOWED_ORIGINS` in `.env`, then `sudo systemctl restart oddvice-api`.

## Updating later
```bash
cd /opt/oddvice-api
sudo git pull
sudo go build -o bin/server ./cmd/server
sudo chown -R oddvice:oddvice /opt/oddvice-api
sudo systemctl restart oddvice-api
```
