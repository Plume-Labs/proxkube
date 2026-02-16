// ProxKube Dashboard Plugin for Proxmox VE
//
// This plugin adds a "ProxKube" panel to the Proxmox VE web interface,
// allowing users to view and manage proxkube-managed LXC containers
// directly from the dashboard. It shows pods filtered by the "proxkube"
// tag, displays their status, and provides basic lifecycle controls.
//
// Installation:
//   1. Copy this directory to /usr/share/pve-manager/proxkube/
//   2. Register the plugin: pveam update
//   3. Reload the Proxmox web interface
//
// Or install via the Makefile:
//   make -C deploy/pve-plugin install

Ext.define('PVE.ProxKube', {
    extend: 'Ext.panel.Panel',
    alias: 'widget.pveProxKube',

    title: 'ProxKube Pods',
    iconCls: 'fa fa-cubes',
    layout: 'fit',

    // Filter tag used to identify proxkube-managed containers.
    proxkubeTag: 'proxkube',

    initComponent: function() {
        var me = this;

        me.store = Ext.create('Ext.data.Store', {
            fields: [
                'vmid', 'name', 'status', 'node', 'tags',
                'cpu', 'mem', 'maxmem', 'disk', 'maxdisk',
                'uptime', 'ip', 'pool', 'description'
            ],
            sorters: [{ property: 'vmid', direction: 'ASC' }]
        });

        me.grid = Ext.create('Ext.grid.Panel', {
            store: me.store,
            border: false,
            columns: [
                {
                    text: 'VMID',
                    dataIndex: 'vmid',
                    width: 70,
                    align: 'right'
                },
                {
                    text: 'Name',
                    dataIndex: 'name',
                    flex: 1,
                    renderer: function(value, meta, record) {
                        var status = record.get('status');
                        var icon = status === 'running' ? 'fa-play-circle green' : 'fa-stop-circle red';
                        return '<i class="fa ' + Ext.htmlEncode(icon) + '"></i> ' + Ext.htmlEncode(value);
                    }
                },
                {
                    text: 'Status',
                    dataIndex: 'status',
                    width: 90,
                    renderer: function(value) {
                        var cls = value === 'running' ? 'proxkube-status-running' : 'proxkube-status-stopped';
                        return '<span class="' + Ext.htmlEncode(cls) + '">' + Ext.htmlEncode(value) + '</span>';
                    }
                },
                {
                    text: 'Node',
                    dataIndex: 'node',
                    width: 100
                },
                {
                    text: 'IP',
                    dataIndex: 'ip',
                    width: 130,
                    renderer: function(value) {
                        return value || '<em>—</em>';
                    }
                },
                {
                    text: 'CPU',
                    dataIndex: 'cpu',
                    width: 70,
                    align: 'right',
                    renderer: function(value) {
                        if (!value && value !== 0) return '—';
                        return (value * 100).toFixed(1) + '%';
                    }
                },
                {
                    text: 'Memory',
                    dataIndex: 'mem',
                    width: 110,
                    renderer: function(value, meta, record) {
                        var max = record.get('maxmem');
                        if (!max) return '—';
                        var used = (value / max * 100).toFixed(0);
                        var mb = (value / 1024 / 1024).toFixed(0);
                        var maxMb = (max / 1024 / 1024).toFixed(0);
                        return mb + ' / ' + maxMb + ' MB (' + used + '%)';
                    }
                },
                {
                    text: 'Tags',
                    dataIndex: 'tags',
                    flex: 1,
                    renderer: function(value) {
                        if (!value) return '';
                        var tags = value.split(';');
                        var html = '';
                        for (var i = 0; i < tags.length; i++) {
                            var tag = Ext.htmlEncode(tags[i].trim());
                            if (tag) {
                                var cls = tag === 'proxkube' ? 'proxkube-tag proxkube-tag-primary' : 'proxkube-tag';
                                html += '<span class="' + cls + '">' + tag + '</span> ';
                            }
                        }
                        return html;
                    }
                },
                {
                    text: 'Uptime',
                    dataIndex: 'uptime',
                    width: 100,
                    renderer: function(value) {
                        if (!value) return '—';
                        var h = Math.floor(value / 3600);
                        var m = Math.floor((value % 3600) / 60);
                        if (h > 24) {
                            var d = Math.floor(h / 24);
                            h = h % 24;
                            return d + 'd ' + h + 'h';
                        }
                        return h + 'h ' + m + 'm';
                    }
                }
            ],
            tbar: [
                {
                    text: 'Refresh',
                    iconCls: 'fa fa-refresh',
                    handler: function() {
                        me.loadPods();
                    }
                },
                '-',
                {
                    text: 'Start',
                    iconCls: 'fa fa-play',
                    disabled: true,
                    itemId: 'startBtn',
                    handler: function() {
                        me.doAction('start');
                    }
                },
                {
                    text: 'Stop',
                    iconCls: 'fa fa-stop',
                    disabled: true,
                    itemId: 'stopBtn',
                    handler: function() {
                        me.doAction('stop');
                    }
                },
                {
                    text: 'Delete',
                    iconCls: 'fa fa-trash',
                    disabled: true,
                    itemId: 'deleteBtn',
                    handler: function() {
                        me.doAction('delete');
                    }
                },
                '->',
                {
                    xtype: 'textfield',
                    itemId: 'searchField',
                    emptyText: 'Filter pods...',
                    width: 200,
                    enableKeyEvents: true,
                    listeners: {
                        keyup: function(field) {
                            me.filterPods(field.getValue());
                        }
                    }
                }
            ],

            listeners: {
                selectionchange: function(sm, records) {
                    var hasSelection = records.length > 0;
                    var selected = records[0];
                    me.grid.down('#startBtn').setDisabled(!hasSelection || (selected && selected.get('status') === 'running'));
                    me.grid.down('#stopBtn').setDisabled(!hasSelection || (selected && selected.get('status') !== 'running'));
                    me.grid.down('#deleteBtn').setDisabled(!hasSelection);
                }
            }
        });

        Ext.apply(me, {
            items: [me.grid]
        });

        me.callParent();

        // Initial load.
        me.loadPods();

        // Auto-refresh every 30 seconds.
        me.refreshTask = Ext.TaskManager.start({
            run: function() { me.loadPods(); },
            interval: 30000
        });

        me.on('destroy', function() {
            Ext.TaskManager.stop(me.refreshTask);
        });
    },

    // loadPods fetches all LXC containers from all nodes and filters for
    // those tagged with "proxkube".
    loadPods: function() {
        var me = this;

        Proxmox.Utils.API2Request({
            url: '/cluster/resources',
            params: { type: 'lxc' },
            method: 'GET',
            success: function(response) {
                var data = response.result.data || [];
                var pods = [];

                for (var i = 0; i < data.length; i++) {
                    var ct = data[i];
                    var tags = ct.tags || '';
                    // Only show proxkube-managed containers.
                    if (tags.indexOf(me.proxkubeTag) === -1) {
                        continue;
                    }
                    pods.push({
                        vmid: ct.vmid,
                        name: ct.name || 'CT ' + ct.vmid,
                        status: ct.status,
                        node: ct.node,
                        tags: tags,
                        cpu: ct.cpu,
                        mem: ct.mem,
                        maxmem: ct.maxmem,
                        disk: ct.disk,
                        maxdisk: ct.maxdisk,
                        uptime: ct.uptime,
                        pool: ct.pool || '',
                        description: ''
                    });
                }

                me.store.loadData(pods);
                // Fetch IPs for running containers.
                me.loadIPs(pods);
            },
            failure: function(response) {
                Ext.Msg.alert('Error', 'Failed to load ProxKube pods: ' + (response.htmlStatus || 'unknown error'));
            }
        });
    },

    // loadIPs fetches IP addresses for running containers.
    loadIPs: function(pods) {
        var me = this;
        for (var i = 0; i < pods.length; i++) {
            (function(pod) {
                if (pod.status !== 'running') return;
                Proxmox.Utils.API2Request({
                    url: '/nodes/' + encodeURIComponent(pod.node) + '/lxc/' + encodeURIComponent(pod.vmid) + '/interfaces',
                    method: 'GET',
                    success: function(response) {
                        var ifaces = response.result.data || [];
                        for (var j = 0; j < ifaces.length; j++) {
                            if (ifaces[j].name !== 'lo' && ifaces[j].inet) {
                                var ip = ifaces[j].inet;
                                var slashIdx = ip.indexOf('/');
                                if (slashIdx > 0) ip = ip.substring(0, slashIdx);
                                var record = me.store.findRecord('vmid', pod.vmid);
                                if (record) {
                                    record.set('ip', ip);
                                    record.commit();
                                }
                                break;
                            }
                        }
                    }
                });
            })(pods[i]);
        }
    },

    // filterPods filters the grid by name or tag.
    filterPods: function(query) {
        var me = this;
        me.store.clearFilter();
        if (query) {
            var lowerQuery = query.toLowerCase();
            me.store.filterBy(function(record) {
                var name = (record.get('name') || '').toLowerCase();
                var tags = (record.get('tags') || '').toLowerCase();
                return name.indexOf(lowerQuery) !== -1 || tags.indexOf(lowerQuery) !== -1;
            });
        }
    },

    // doAction performs a lifecycle action (start/stop/delete) on the
    // selected container.
    doAction: function(action) {
        var me = this;
        var record = me.grid.getSelectionModel().getSelection()[0];
        if (!record) return;

        var vmid = record.get('vmid');
        var node = record.get('node');
        var name = record.get('name');
        var actionText = action.charAt(0).toUpperCase() + action.slice(1);

        Ext.Msg.confirm(
            actionText + ' Pod',
            'Are you sure you want to ' + action + ' pod "' + Ext.htmlEncode(name) + '" (VMID ' + vmid + ')?',
            function(btn) {
                if (btn !== 'yes') return;

                var url, method;
                if (action === 'delete') {
                    url = '/nodes/' + encodeURIComponent(node) + '/lxc/' + encodeURIComponent(vmid);
                    method = 'DELETE';
                } else {
                    url = '/nodes/' + encodeURIComponent(node) + '/lxc/' + encodeURIComponent(vmid) + '/status/' + encodeURIComponent(action);
                    method = 'POST';
                }

                Proxmox.Utils.API2Request({
                    url: url,
                    method: method,
                    success: function() {
                        // Refresh after a short delay to allow the task to complete.
                        Ext.defer(function() { me.loadPods(); }, 2000);
                    },
                    failure: function(response) {
                        Ext.Msg.alert('Error', 'Failed to ' + action + ' pod: ' + (response.htmlStatus || 'unknown error'));
                    }
                });
            }
        );
    }
});
