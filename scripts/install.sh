#!/bin/sh
# proxkube install script
# Usage: sudo ./scripts/install.sh
set -e

PREFIX="${PREFIX:-/usr/local}"
BINDIR="${PREFIX}/bin"
SYSTEMD_DIR="/usr/lib/systemd/system"
SHARE_DIR="/usr/share/proxkube"
PVE_SHARE="/usr/share/pve-manager"
PERL_DIR="/usr/share/perl5/PVE/API2"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Installing proxkube"

# Build if binary is missing.
if [ ! -f "$REPO_DIR/proxkube" ]; then
    echo "    Building proxkube binary..."
    cd "$REPO_DIR"
    go build -o proxkube ./cmd/proxkube
fi

# Install binary.
install -d "$BINDIR"
install -m 0755 "$REPO_DIR/proxkube" "$BINDIR/proxkube"
echo "    Installed proxkube to $BINDIR/proxkube"

# Install systemd service.
if [ -d "$(dirname "$SYSTEMD_DIR")" ]; then
    install -d "$SYSTEMD_DIR"
    install -m 0644 "$SCRIPT_DIR/proxkube-daemon.service" "$SYSTEMD_DIR/proxkube-daemon.service"
    systemctl daemon-reload
    echo "    Installed proxkube-daemon.service"
    echo "    Enable with: systemctl enable --now proxkube-daemon"
fi

# Install PVE dashboard plugin (only when Proxmox is detected).
if [ -d "$PVE_SHARE" ]; then
    echo "==> Installing PVE dashboard plugin"
    install -d "$PVE_SHARE/proxkube"
    install -m 0644 "$REPO_DIR/deploy/pve-plugin/ProxKubePanel.js" "$PVE_SHARE/proxkube/"
    install -m 0644 "$REPO_DIR/deploy/pve-plugin/proxkube.css"     "$PVE_SHARE/proxkube/"
    if [ -d "$PERL_DIR" ]; then
        install -m 0644 "$REPO_DIR/deploy/pve-plugin/ProxKube.pm" "$PERL_DIR/ProxKube.pm"
    fi
    systemctl restart pvedaemon pveproxy 2>/dev/null || true
    echo "    PVE plugin installed. Reload the web interface."
fi

# Install shared resources.
install -d "$SHARE_DIR"
cp -r "$REPO_DIR/deploy" "$SHARE_DIR/"
echo "    Shared resources installed to $SHARE_DIR"

echo "==> proxkube installation complete"
