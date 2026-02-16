# ProxKube PVE Dashboard Plugin — Makefile
#
# Install the plugin on a Proxmox VE host:
#   make install
#
# Remove it:
#   make uninstall

PVE_SHARE  := /usr/share/pve-manager
PLUGIN_DIR := $(PVE_SHARE)/proxkube
PERL_DIR   := /usr/share/perl5/PVE/API2

.PHONY: install uninstall

install:
	@echo "==> Installing ProxKube dashboard plugin"
	install -d $(PLUGIN_DIR)
	install -m 0644 ProxKubePanel.js $(PLUGIN_DIR)/
	install -m 0644 proxkube.css     $(PLUGIN_DIR)/
	install -m 0644 ProxKube.pm      $(PERL_DIR)/ProxKube.pm
	@echo "==> Restarting PVE services"
	systemctl restart pvedaemon pveproxy
	@echo "==> ProxKube plugin installed. Reload the web interface."

uninstall:
	@echo "==> Removing ProxKube dashboard plugin"
	rm -rf $(PLUGIN_DIR)
	rm -f  $(PERL_DIR)/ProxKube.pm
	systemctl restart pvedaemon pveproxy
	@echo "==> ProxKube plugin removed."
