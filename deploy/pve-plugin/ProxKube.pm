package PVE::API2::ProxKube;

use strict;
use warnings;

use PVE::RESTHandler;
use PVE::JSONSchema qw(get_standard_option);
use PVE::Cluster;
use PVE::LXC;

use base qw(PVE::RESTHandler);

# ProxKube plugin registration for Proxmox VE.
#
# This module registers the ProxKube panel with the Proxmox VE web
# interface, enabling the dashboard tab that shows proxkube-managed
# containers (identified by the "proxkube" tag). It also provides
# endpoints for plugin status, pod creation, and daemon management.
#
# Installation:
#   cp ProxKube.pm /usr/share/perl5/PVE/API2/
#   systemctl restart pvedaemon pveproxy

# --- List proxkube pods ---

__PACKAGE__->register_method({
    name      => 'index',
    path      => '',
    method    => 'GET',
    description => 'List ProxKube-managed containers (those tagged with "proxkube").',
    permissions => {
        check => ['perm', '/', ['Sys.Audit']],
    },
    parameters => {
        additionalProperties => 0,
        properties => {
            node => get_standard_option('pve-node', { optional => 1 }),
        },
    },
    returns => {
        type => 'array',
        items => {
            type   => 'object',
            properties => {
                vmid   => { type => 'integer' },
                name   => { type => 'string' },
                status => { type => 'string' },
                node   => { type => 'string' },
                tags   => { type => 'string', optional => 1 },
            },
        },
    },
    code => sub {
        my ($param) = @_;

        my $result = [];
        my $rpcenv = PVE::RPCEnvironment::get();

        # Iterate cluster resources and filter by the proxkube tag.
        my $resources = PVE::Cluster::get_resources('lxc');
        foreach my $ct (@$resources) {
            my $tags = $ct->{tags} // '';
            next unless $tags =~ /\bproxkube\b/;

            if ($param->{node}) {
                next unless $ct->{node} eq $param->{node};
            }

            push @$result, {
                vmid   => $ct->{vmid},
                name   => $ct->{name} // "CT $ct->{vmid}",
                status => $ct->{status},
                node   => $ct->{node},
                tags   => $tags,
            };
        }

        return $result;
    },
});

# --- Plugin status ---

__PACKAGE__->register_method({
    name      => 'status',
    path      => 'status',
    method    => 'GET',
    description => 'Get ProxKube plugin and daemon status.',
    permissions => {
        check => ['perm', '/', ['Sys.Audit']],
    },
    parameters => {
        additionalProperties => 0,
        properties => {},
    },
    returns => {
        type => 'object',
        properties => {
            version        => { type => 'string' },
            plugin         => { type => 'string' },
            daemon         => { type => 'string' },
            pod_count      => { type => 'integer' },
            running_count  => { type => 'integer' },
            stopped_count  => { type => 'integer' },
        },
    },
    code => sub {
        my ($param) = @_;

        # Detect proxkube binary version.
        my $version = 'unknown';
        if (-x '/usr/local/bin/proxkube') {
            $version = `proxkube version 2>/dev/null` // 'unknown';
            chomp $version;
        } elsif (-x '/usr/bin/proxkube') {
            $version = `proxkube version 2>/dev/null` // 'unknown';
            chomp $version;
        }

        # Plugin installed?
        my $plugin_status = 'not installed';
        if (-f '/usr/share/pve-manager/proxkube/ProxKubePanel.js') {
            $plugin_status = 'installed';
        }

        # Daemon status.
        my $daemon_status = 'inactive';
        my $systemctl_out = `systemctl is-active proxkube-daemon 2>/dev/null` // '';
        chomp $systemctl_out;
        $daemon_status = $systemctl_out if $systemctl_out;

        # Count pods.
        my ($pod_count, $running_count, $stopped_count) = (0, 0, 0);
        my $resources = PVE::Cluster::get_resources('lxc');
        foreach my $ct (@$resources) {
            my $tags = $ct->{tags} // '';
            next unless $tags =~ /\bproxkube\b/;
            $pod_count++;
            if ($ct->{status} eq 'running') {
                $running_count++;
            } else {
                $stopped_count++;
            }
        }

        return {
            version       => $version,
            plugin        => $plugin_status,
            daemon        => $daemon_status,
            pod_count     => $pod_count,
            running_count => $running_count,
            stopped_count => $stopped_count,
        };
    },
});

# --- Daemon control ---

__PACKAGE__->register_method({
    name      => 'daemon_control',
    path      => 'daemon/{action}',
    method    => 'POST',
    description => 'Control the ProxKube daemon (start, stop, restart, enable, disable).',
    permissions => {
        check => ['perm', '/', ['Sys.Modify']],
    },
    parameters => {
        additionalProperties => 0,
        properties => {
            action => {
                type => 'string',
                enum => ['start', 'stop', 'restart', 'enable', 'disable'],
                description => 'Daemon action to perform.',
            },
        },
    },
    returns => {
        type => 'object',
        properties => {
            success => { type => 'boolean' },
            message => { type => 'string' },
        },
    },
    code => sub {
        my ($param) = @_;
        my $action = $param->{action};

        my $cmd;
        if ($action eq 'enable') {
            $cmd = 'systemctl enable --now proxkube-daemon 2>&1';
        } elsif ($action eq 'disable') {
            $cmd = 'systemctl disable --now proxkube-daemon 2>&1';
        } else {
            $cmd = "systemctl $action proxkube-daemon 2>&1";
        }

        my $output = `$cmd`;
        my $rc = $? >> 8;

        return {
            success => ($rc == 0) ? 1 : 0,
            message => $rc == 0 ? "Daemon ${action}ed successfully" : "Failed: $output",
        };
    },
});

# --- Create pod ---

__PACKAGE__->register_method({
    name      => 'create_pod',
    path      => 'pods',
    method    => 'POST',
    description => 'Create a new ProxKube pod (LXC container with proxkube tag).',
    permissions => {
        check => ['perm', '/', ['VM.Allocate']],
    },
    parameters => {
        additionalProperties => 0,
        properties => {
            node => get_standard_option('pve-node'),
            name => {
                type => 'string',
                description => 'Pod name (used as hostname).',
            },
            image => {
                type => 'string',
                description => 'OCI image or LXC template.',
            },
            cpu => {
                type => 'integer',
                minimum => 1,
                maximum => 128,
                default => 1,
                optional => 1,
                description => 'Number of CPU cores.',
            },
            memory => {
                type => 'integer',
                minimum => 64,
                default => 512,
                optional => 1,
                description => 'Memory in MB.',
            },
            disk => {
                type => 'integer',
                minimum => 1,
                default => 8,
                optional => 1,
                description => 'Root disk size in GB.',
            },
            storage => {
                type => 'string',
                default => 'local-lvm',
                optional => 1,
                description => 'Proxmox storage pool.',
            },
            bridge => {
                type => 'string',
                default => 'vmbr0',
                optional => 1,
                description => 'Network bridge.',
            },
            tags => {
                type => 'string',
                optional => 1,
                description => 'Additional tags (semicolon-separated).',
            },
            pool => {
                type => 'string',
                optional => 1,
                description => 'Resource pool.',
            },
            description => {
                type => 'string',
                optional => 1,
                description => 'Pod description.',
            },
        },
    },
    returns => {
        type => 'object',
        properties => {
            vmid => { type => 'integer' },
            name => { type => 'string' },
        },
    },
    code => sub {
        my ($param) = @_;

        my $node    = $param->{node};
        my $name    = $param->{name};
        my $image   = $param->{image};
        my $cpu     = $param->{cpu} // 1;
        my $memory  = $param->{memory} // 512;
        my $disk    = $param->{disk} // 8;
        my $storage = $param->{storage} // 'local-lvm';
        my $bridge  = $param->{bridge} // 'vmbr0';
        my $pool    = $param->{pool} // '';
        my $desc    = $param->{description} // "Managed by proxkube\nName: $name";

        # Build tags — always include "proxkube".
        my $tags = 'proxkube';
        if ($param->{tags}) {
            $tags .= ';' . $param->{tags};
        }

        # Allocate VMID.
        my $vmid = PVE::Cluster::get_next_vmid();

        # Build LXC config and create via PVE API.
        my $conf = {
            vmid        => $vmid,
            ostemplate  => $image,
            hostname    => $name,
            cores       => $cpu,
            memory      => $memory,
            rootfs      => "$storage:$disk",
            net0        => "name=eth0,bridge=$bridge,ip=dhcp",
            tags        => $tags,
            description => $desc,
            start       => 1,
        };
        $conf->{pool} = $pool if $pool;

        PVE::LXC::create_lxc($node, $conf);

        return {
            vmid => $vmid,
            name => $name,
        };
    },
});

1;
