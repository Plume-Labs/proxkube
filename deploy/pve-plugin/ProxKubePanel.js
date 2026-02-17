// ProxKube Dashboard Plugin for Proxmox VE
//
// This plugin adds a "ProxKube" panel to the Proxmox VE web interface,
// allowing users to view and manage proxkube-managed LXC containers
// directly from the dashboard. It provides:
//
//   - Pod grid with lifecycle controls (start/stop/delete)
//   - Create Pod dialog for deploying new containers
//   - Plugin & daemon management tab (status, start/stop/restart/enable/disable)
//   - Pod detail panel showing resources, tags, and description
//   - Real-time status refresh and search/filter
//
// Installation:
//   make -C deploy/pve-plugin install

// ─── Create Pod Dialog ─────────────────────────────────────────────────

Ext.define('PVE.ProxKubeCreatePod', {
    extend: 'Ext.window.Window',
    alias: 'widget.pveProxKubeCreatePod',

    title: 'Create ProxKube Pod',
    iconCls: 'fa fa-plus-circle',
    modal: true,
    width: 500,
    layout: 'fit',
    resizable: false,

    initComponent: function() {
        var me = this;

        me.form = Ext.create('Ext.form.Panel', {
            bodyPadding: 15,
            border: false,
            defaults: {
                anchor: '100%',
                labelWidth: 100
            },
            items: [
                {
                    xtype: 'textfield',
                    name: 'name',
                    fieldLabel: 'Name',
                    allowBlank: false,
                    emptyText: 'my-pod',
                    regex: /^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$/,
                    regexText: 'Lowercase letters, numbers, and hyphens only (cannot start or end with hyphen)'
                },
                {
                    xtype: 'textfield',
                    name: 'node',
                    fieldLabel: 'Node',
                    allowBlank: false,
                    value: 'pve',
                    emptyText: 'pve'
                },
                {
                    xtype: 'textfield',
                    name: 'image',
                    fieldLabel: 'Image',
                    allowBlank: false,
                    emptyText: 'docker.io/library/nginx:latest'
                },
                {
                    xtype: 'fieldcontainer',
                    fieldLabel: 'Resources',
                    layout: 'hbox',
                    defaults: { margin: '0 5 0 0' },
                    items: [
                        {
                            xtype: 'numberfield',
                            name: 'cpu',
                            fieldLabel: 'CPU',
                            labelWidth: 30,
                            width: 100,
                            value: 1,
                            minValue: 1,
                            maxValue: 128
                        },
                        {
                            xtype: 'numberfield',
                            name: 'memory',
                            fieldLabel: 'RAM',
                            labelWidth: 30,
                            width: 120,
                            value: 512,
                            minValue: 64,
                            step: 128
                        },
                        {
                            xtype: 'numberfield',
                            name: 'disk',
                            fieldLabel: 'Disk',
                            labelWidth: 30,
                            width: 110,
                            value: 8,
                            minValue: 1
                        }
                    ]
                },
                {
                    xtype: 'textfield',
                    name: 'storage',
                    fieldLabel: 'Storage',
                    value: 'local-lvm',
                    emptyText: 'local-lvm'
                },
                {
                    xtype: 'textfield',
                    name: 'bridge',
                    fieldLabel: 'Bridge',
                    value: 'vmbr0',
                    emptyText: 'vmbr0'
                },
                {
                    xtype: 'textfield',
                    name: 'tags',
                    fieldLabel: 'Tags',
                    emptyText: 'web;production (semicolon-separated)'
                },
                {
                    xtype: 'textfield',
                    name: 'pool',
                    fieldLabel: 'Pool',
                    emptyText: 'Optional resource pool'
                },
                {
                    xtype: 'textarea',
                    name: 'description',
                    fieldLabel: 'Description',
                    emptyText: 'Optional description',
                    height: 60
                }
            ],
            buttons: [
                {
                    text: 'Create',
                    formBind: true,
                    iconCls: 'fa fa-check',
                    handler: function() {
                        me.doCreate();
                    }
                },
                {
                    text: 'Cancel',
                    iconCls: 'fa fa-times',
                    handler: function() {
                        me.close();
                    }
                }
            ]
        });

        Ext.apply(me, {
            items: [me.form]
        });

        me.callParent();
    },

    doCreate: function() {
        var me = this;
        var values = me.form.getForm().getValues();

        Proxmox.Utils.API2Request({
            url: '/api2/extjs/proxkube/pods',
            method: 'POST',
            params: values,
            waitMsgTarget: me,
            success: function(response) {
                var data = response.result.data || {};
                Ext.Msg.alert('Success',
                    'Pod "' + Ext.htmlEncode(data.name) + '" created (VMID ' + data.vmid + ')');
                me.close();
                if (me.onCreateSuccess) {
                    me.onCreateSuccess();
                }
            },
            failure: function(response) {
                Ext.Msg.alert('Error',
                    'Failed to create pod: ' + (response.htmlStatus || 'unknown error'));
            }
        });
    }
});

// ─── Pod Detail Panel ───────────────────────────────────────────────────

Ext.define('PVE.ProxKubePodDetail', {
    extend: 'Ext.panel.Panel',
    alias: 'widget.pveProxKubePodDetail',

    title: 'Pod Details',
    iconCls: 'fa fa-info-circle',
    bodyPadding: 10,
    cls: 'proxkube-detail-panel',

    tpl: new Ext.XTemplate(
        '<div class="proxkube-detail">',
        '  <h3><i class="fa fa-cube"></i> {name}</h3>',
        '  <table class="proxkube-detail-table">',
        '    <tr><td class="proxkube-detail-label">VMID</td><td>{vmid}</td></tr>',
        '    <tr><td class="proxkube-detail-label">Status</td><td><span class="proxkube-status-{status}">{status}</span></td></tr>',
        '    <tr><td class="proxkube-detail-label">Node</td><td>{node}</td></tr>',
        '    <tr><td class="proxkube-detail-label">IP</td><td>{[values.ip || "—"]}</td></tr>',
        '    <tr><td class="proxkube-detail-label">CPU</td><td>{[values.cpu !== undefined ? (values.cpu * 100).toFixed(1) + "%" : "—"]}</td></tr>',
        '    <tr><td class="proxkube-detail-label">Memory</td>',
        '      <td>{[values.maxmem ? (Math.round(values.mem/1048576) + " / " + Math.round(values.maxmem/1048576) + " MB") : "—"]}</td></tr>',
        '    <tr><td class="proxkube-detail-label">Disk</td>',
        '      <td>{[values.maxdisk ? (Math.round(values.disk/1073741824) + " / " + Math.round(values.maxdisk/1073741824) + " GB") : "—"]}</td></tr>',
        '    <tr><td class="proxkube-detail-label">Uptime</td>',
        '      <td>{[values.uptime ? (Math.floor(values.uptime/3600) + "h " + Math.floor((values.uptime%3600)/60) + "m") : "—"]}</td></tr>',
        '    <tr><td class="proxkube-detail-label">Pool</td><td>{[values.pool || "—"]}</td></tr>',
        '    <tr><td class="proxkube-detail-label">Tags</td><td>{[values.tags || "—"]}</td></tr>',
        '  </table>',
        '</div>'
    ),

    updateDetail: function(record) {
        if (record) {
            this.update(record.data);
        } else {
            this.update({});
        }
    },

    initComponent: function() {
        this.html = '<div class="proxkube-detail-empty"><i class="fa fa-hand-pointer-o"></i> Select a pod to view details</div>';
        this.callParent();
    }
});

// ─── Plugin Management Tab ──────────────────────────────────────────────

Ext.define('PVE.ProxKubeManagement', {
    extend: 'Ext.panel.Panel',
    alias: 'widget.pveProxKubeManagement',

    title: 'Plugin Management',
    iconCls: 'fa fa-cogs',
    bodyPadding: 15,
    autoScroll: true,

    initComponent: function() {
        var me = this;

        me.statusTpl = new Ext.XTemplate(
            '<div class="proxkube-mgmt">',
            '  <div class="proxkube-mgmt-section">',
            '    <h3><i class="fa fa-info-circle"></i> ProxKube Status</h3>',
            '    <table class="proxkube-detail-table">',
            '      <tr><td class="proxkube-detail-label">Version</td><td>{version}</td></tr>',
            '      <tr><td class="proxkube-detail-label">Plugin</td>',
            '        <td><span class="proxkube-badge proxkube-badge-{[values.plugin === "installed" ? "ok" : "warn"]}">{plugin}</span></td></tr>',
            '      <tr><td class="proxkube-detail-label">Daemon</td>',
            '        <td><span class="proxkube-badge proxkube-badge-{[values.daemon === "active" ? "ok" : "warn"]}">{daemon}</span></td></tr>',
            '    </table>',
            '  </div>',
            '  <div class="proxkube-mgmt-section">',
            '    <h3><i class="fa fa-cubes"></i> Pod Statistics</h3>',
            '    <table class="proxkube-detail-table">',
            '      <tr><td class="proxkube-detail-label">Total Pods</td><td>{pod_count}</td></tr>',
            '      <tr><td class="proxkube-detail-label">Running</td><td class="proxkube-status-running">{running_count}</td></tr>',
            '      <tr><td class="proxkube-detail-label">Stopped</td><td class="proxkube-status-stopped">{stopped_count}</td></tr>',
            '    </table>',
            '  </div>',
            '</div>'
        );

        me.statusPanel = Ext.create('Ext.panel.Panel', {
            border: false,
            html: '<div class="proxkube-detail-empty"><i class="fa fa-spinner fa-spin"></i> Loading status...</div>'
        });

        me.daemonToolbar = Ext.create('Ext.toolbar.Toolbar', {
            cls: 'proxkube-daemon-toolbar',
            items: [
                { text: 'Refresh', iconCls: 'fa fa-refresh', handler: function() { me.loadStatus(); } },
                '-',
                { text: 'Start Daemon', iconCls: 'fa fa-play', itemId: 'daemonStart', handler: function() { me.daemonAction('start'); } },
                { text: 'Stop Daemon', iconCls: 'fa fa-stop', itemId: 'daemonStop', handler: function() { me.daemonAction('stop'); } },
                { text: 'Restart Daemon', iconCls: 'fa fa-repeat', itemId: 'daemonRestart', handler: function() { me.daemonAction('restart'); } },
                '-',
                { text: 'Enable on Boot', iconCls: 'fa fa-toggle-on', itemId: 'daemonEnable', handler: function() { me.daemonAction('enable'); } },
                { text: 'Disable on Boot', iconCls: 'fa fa-toggle-off', itemId: 'daemonDisable', handler: function() { me.daemonAction('disable'); } }
            ]
        });

        Ext.apply(me, {
            items: [me.daemonToolbar, me.statusPanel]
        });

        me.callParent();
        me.loadStatus();
    },

    loadStatus: function() {
        var me = this;
        Proxmox.Utils.API2Request({
            url: '/api2/extjs/proxkube/status',
            method: 'GET',
            success: function(response) {
                var data = response.result.data || {};
                me.statusPanel.update(me.statusTpl.apply(data));
            },
            failure: function() {
                me.statusPanel.update('<div class="proxkube-detail-empty"><i class="fa fa-exclamation-triangle"></i> Failed to load status</div>');
            }
        });
    },

    daemonAction: function(action) {
        var me = this;
        Ext.Msg.confirm(
            'Daemon ' + action.charAt(0).toUpperCase() + action.slice(1),
            'Are you sure you want to ' + action + ' the ProxKube daemon?',
            function(btn) {
                if (btn !== 'yes') return;
                Proxmox.Utils.API2Request({
                    url: '/api2/extjs/proxkube/daemon/' + encodeURIComponent(action),
                    method: 'POST',
                    success: function(response) {
                        var data = response.result.data || {};
                        Ext.Msg.alert('Daemon', Ext.htmlEncode(data.message));
                        me.loadStatus();
                    },
                    failure: function(response) {
                        Ext.Msg.alert('Error', 'Failed: ' + (response.htmlStatus || 'unknown error'));
                    }
                });
            }
        );
    }
});

// ─── Main ProxKube Panel (Tab Container) ─────────────────────────────────

Ext.define('PVE.ProxKube', {
    extend: 'Ext.tab.Panel',
    alias: 'widget.pveProxKube',

    title: 'ProxKube',
    iconCls: 'fa fa-cubes',
    tabPosition: 'top',

    // Filter tag used to identify proxkube-managed containers.
    proxkubeTag: 'proxkube',

    initComponent: function() {
        var me = this;

        // ── Pod Store ────────────────────────────────────────────────
        me.store = Ext.create('Ext.data.Store', {
            fields: [
                'vmid', 'name', 'status', 'node', 'tags',
                'cpu', 'mem', 'maxmem', 'disk', 'maxdisk',
                'uptime', 'ip', 'pool', 'description'
            ],
            sorters: [{ property: 'vmid', direction: 'ASC' }]
        });

        // ── Pod Detail Panel ─────────────────────────────────────────
        me.detailPanel = Ext.create('PVE.ProxKubePodDetail', {
            region: 'east',
            width: 280,
            split: true,
            collapsible: true,
            collapsed: false
        });

        // ── Pod Grid ─────────────────────────────────────────────────
        me.grid = Ext.create('Ext.grid.Panel', {
            region: 'center',
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
                {
                    text: 'Create Pod',
                    iconCls: 'fa fa-plus',
                    handler: function() {
                        me.showCreateDialog();
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
                    text: 'Restart',
                    iconCls: 'fa fa-repeat',
                    disabled: true,
                    itemId: 'restartBtn',
                    handler: function() {
                        me.doAction('restart');
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
                    var isRunning = selected && selected.get('status') === 'running';
                    me.grid.down('#startBtn').setDisabled(!hasSelection || isRunning);
                    me.grid.down('#stopBtn').setDisabled(!hasSelection || !isRunning);
                    me.grid.down('#restartBtn').setDisabled(!hasSelection || !isRunning);
                    me.grid.down('#deleteBtn').setDisabled(!hasSelection);
                    me.detailPanel.updateDetail(selected);
                }
            }
        });

        // ── Pods Tab ─────────────────────────────────────────────────
        me.podsTab = Ext.create('Ext.panel.Panel', {
            title: 'Pods',
            iconCls: 'fa fa-cube',
            layout: 'border',
            items: [me.grid, me.detailPanel]
        });

        // ── Management Tab ───────────────────────────────────────────
        me.mgmtTab = Ext.create('PVE.ProxKubeManagement');

        Ext.apply(me, {
            items: [me.podsTab, me.mgmtTab],
            activeTab: 0
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

    // showCreateDialog opens the Create Pod dialog.
    showCreateDialog: function() {
        var me = this;
        Ext.create('PVE.ProxKubeCreatePod', {
            onCreateSuccess: function() {
                // Refresh the pod list after creation.
                Ext.defer(function() { me.loadPods(); }, 2000);
            }
        }).show();
    },

    // doAction performs a lifecycle action (start/stop/restart/delete) on
    // the selected container.
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
                } else if (action === 'restart') {
                    // Stop first, then start.
                    url = '/nodes/' + encodeURIComponent(node) + '/lxc/' + encodeURIComponent(vmid) + '/status/stop';
                    method = 'POST';
                    Proxmox.Utils.API2Request({
                        url: url,
                        method: method,
                        success: function() {
                            Ext.defer(function() {
                                Proxmox.Utils.API2Request({
                                    url: '/nodes/' + encodeURIComponent(node) + '/lxc/' + encodeURIComponent(vmid) + '/status/start',
                                    method: 'POST',
                                    success: function() {
                                        Ext.defer(function() { me.loadPods(); }, 2000);
                                    },
                                    failure: function(response) {
                                        Ext.Msg.alert('Error', 'Failed to start pod: ' + (response.htmlStatus || 'unknown error'));
                                    }
                                });
                            }, 5000);
                        },
                        failure: function(response) {
                            Ext.Msg.alert('Error', 'Failed to stop pod: ' + (response.htmlStatus || 'unknown error'));
                        }
                    });
                    return;
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
