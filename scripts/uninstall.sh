#!/bin/sh
# proxkube uninstall script — one-command removal.
# Usage: sudo ./scripts/uninstall.sh
set -e

PREFIX="${PREFIX:-/usr}"
BINDIR="${PREFIX}/bin"
SYSTEMD_DIR="/usr/lib/systemd/system"
PVE_SHARE="/usr/share/pve-manager"
PERL_DIR="/usr/share/perl5/PVE/API2"
PVE_CONF="/etc/pve/proxkube"
DEFAULTS="/etc/default/proxkube"

echo "==> Uninstalling proxkube"

# Stop and disable daemon.
systemctl stop proxkube-daemon 2>/dev/null || true
systemctl disable proxkube-daemon 2>/dev/null || true
echo "    Daemon stopped and disabled."

# Remove binary.
if [ -f "$BINDIR/proxkube" ]; then
    rm -f "$BINDIR/proxkube"
    echo "    Removed $BINDIR/proxkube"
fi

# Remove systemd service.
if [ -f "$SYSTEMD_DIR/proxkube-daemon.service" ]; then
    rm -f "$SYSTEMD_DIR/proxkube-daemon.service"
    systemctl daemon-reload
    echo "    Removed proxkube-daemon.service"
fi

# Remove PVE dashboard plugin.
if [ -d "$PVE_SHARE/proxkube" ]; then
    rm -rf "$PVE_SHARE/proxkube"
    echo "    Removed PVE dashboard plugin"
fi
if [ -f "$PERL_DIR/ProxKube.pm" ]; then
    rm -f "$PERL_DIR/ProxKube.pm"
fi
if systemctl is-active --quiet pvedaemon 2>/dev/null; then
    systemctl restart pvedaemon pveproxy 2>/dev/null || true
fi

# Remove cluster-wide plugin assets.
if [ -d "$PVE_CONF" ]; then
    rm -rf "$PVE_CONF"
    echo "    Removed $PVE_CONF"
fi

# Remove environment file.
if [ -f "$DEFAULTS" ]; then
    rm -f "$DEFAULTS"
    echo "    Removed $DEFAULTS"
fi

echo "==> proxkube uninstalled"
