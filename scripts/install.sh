#!/bin/sh
# proxkube install script - one-command setup.
# Usage: sudo ./scripts/install.sh
#
# Installs the binary, systemd service, PVE dashboard plugin, and
# enables local mode so no API tokens are needed on the Proxmox host.
set -e

PREFIX="${PREFIX:-/usr}"
BINDIR="${PREFIX}/bin"
SYSTEMD_DIR="/usr/lib/systemd/system"
PVE_SHARE="/usr/share/pve-manager"
PERL_DIR="/usr/share/perl5/PVE/API2"
PVE_CONF="/etc/pve/proxkube"
DEFAULTS="/etc/default/proxkube"
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
    echo "    PVE plugin installed."

    # Copy plugin assets to the Proxmox cluster filesystem so other nodes
    # can install the same version without downloading anything.
    if [ -d /etc/pve ]; then
        mkdir -p "$PVE_CONF"
        cp "$REPO_DIR/deploy/pve-plugin/ProxKubePanel.js" "$PVE_CONF/"
        cp "$REPO_DIR/deploy/pve-plugin/proxkube.css"     "$PVE_CONF/"
        cp "$REPO_DIR/deploy/pve-plugin/ProxKube.pm"      "$PVE_CONF/"
        echo "    Plugin assets copied to $PVE_CONF for cluster distribution."
    fi

    systemctl restart pvedaemon pveproxy 2>/dev/null || true
    echo "    PVE services restarted. Reload the web interface."
fi

# Configure local mode - uses PVE Unix socket + pct CLI, no API tokens.
if [ ! -f "$DEFAULTS" ]; then
    NODE_NAME="$(hostname -s 2>/dev/null || echo pve)"
    cat > "$DEFAULTS" <<EOF
# proxkube daemon environment - sourced by the systemd unit.
# Local mode uses the PVE Unix socket and pct CLI; no API tokens needed.
PROXMOX_LOCAL=true
PROXMOX_NODE=${NODE_NAME}
EOF
    echo "    Created $DEFAULTS (PROXMOX_LOCAL=true, node=${NODE_NAME})."
fi

# Enable and start the daemon.
if systemctl enable --now proxkube-daemon 2>/dev/null; then
    echo "    Daemon enabled and started."
else
    echo "    Warning: failed to enable and start proxkube-daemon. Please check systemctl status." >&2
fi

echo "==> proxkube installation complete"
