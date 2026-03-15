#!/bin/bash
# NORA Server — instalační skript
# Použití: curl -fsSL https://noraproject.eu/install.sh | bash

set -e

INSTALL_DIR="/opt/nora"
SERVICE_NAME="nora"
DOWNLOAD_URL="https://noraproject.eu/nora-server"

echo ""
echo "  ╔══════════════════════════════════════╗"
echo "  ║   NORA Server — instalace            ║"
echo "  ╚══════════════════════════════════════╝"
echo ""

# Kontrola root
if [ "$(id -u)" -ne 0 ]; then
    echo "Spusť jako root: curl -fsSL https://noraproject.eu/install.sh | sudo bash"
    exit 1
fi

# Vytvoření adresáře
echo "[1/4] Vytvářím $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR/data/uploads"

# Stažení binárky
echo "[2/4] Stahuji server..."
curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/nora"
chmod +x "$INSTALL_DIR/nora"

# Generování konfigurace (jen pokud neexistuje)
if [ ! -f "$INSTALL_DIR/nora.toml" ]; then
    echo "[3/4] Generuji konfiguraci..."
    JWT_SECRET=$(head -c 32 /dev/urandom | xxd -p)
    cat > "$INSTALL_DIR/nora.toml" << EOF
[server]
host = "0.0.0.0"
port = 9021
name = "My NORA Server"

[database]
path = "data/nora.db"

[auth]
jwt_secret = "$JWT_SECRET"
access_token_ttl = "15m"
refresh_token_ttl = "168h"
challenge_ttl = "5m"

[uploads]
dir = "data/uploads"
max_size_mb = 50

[ratelimit]
requests_per_second = 10
burst = 30

[registration]
open = true
EOF
else
    echo "[3/4] Konfigurace už existuje, přeskakuji."
fi

# Systemd service
echo "[4/4] Vytvářím systemd service..."
cat > /etc/systemd/system/$SERVICE_NAME.service << EOF
[Unit]
Description=NORA Server
After=network.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/nora
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable $SERVICE_NAME
systemctl start $SERVICE_NAME

echo ""
echo "  NORA server běží na portu 9021."
echo "  Konfigurace: $INSTALL_DIR/nora.toml"
echo "  Logy:        journalctl -u $SERVICE_NAME -f"
echo ""
echo "  Připoj se klientem na IP:9021"
echo ""
