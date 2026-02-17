#!/bin/sh
# proxkube uninstall script
# Usage: sudo ./scripts/uninstall.sh
set -e

PREFIX="${PREFIX:-/usr/local}"
BINDIR="${PREFIX}/bin"
SYSTEMD_DIR="/usr/lib/systemd/system"
SHARE_DIR="/usr/share/proxkube"
PVE_SHARE="/usr/share/pve-manager"
PERL_DIR="/usr/share/perl5/PVE/API2"

echo "==> Uninstalling proxkube"

# Stop and disable daemon.
if systemctl is-active proxkube-daemon >/dev/null 2>&1; then
    systemctl stop proxkube-daemon
fi
if systemctl is-enabled proxkube-daemon >/dev/null 2>&1; then
    systemctl disable proxkube-daemon
fi

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
if [ -d "$PVE_SHARE" ]; then
    systemctl restart pvedaemon pveproxy 2>/dev/null || true
fi

# Remove shared resources.
if [ -d "$SHARE_DIR" ]; then
    rm -rf "$SHARE_DIR"
    echo "    Removed $SHARE_DIR"
fi

echo "==> proxkube uninstalled"
