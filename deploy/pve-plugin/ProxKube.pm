package PVE::API2::ProxKube;

use strict;
use warnings;

use PVE::RESTHandler;
use PVE::JSONSchema qw(get_standard_option);
use PVE::Cluster;
use PVE::Tools;

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
        my $proxkube_bin;
        if (-x '/usr/bin/proxkube') {
            $proxkube_bin = '/usr/bin/proxkube';
        } elsif (-x '/usr/local/bin/proxkube') {
            $proxkube_bin = '/usr/local/bin/proxkube';
        }
        if ($proxkube_bin) {
            eval {
                PVE::Tools::run_command([$proxkube_bin, 'version'],
                    outfunc => sub { $version = $_[0]; });
            };
            chomp $version if $version;
        }

        # Plugin installed?
        my $plugin_status = 'not installed';
        if (-f '/usr/share/pve-manager/proxkube/ProxKubePanel.js') {
            $plugin_status = 'installed';
        }

        # Daemon status.
        my $daemon_status = 'inactive';
        eval {
            PVE::Tools::run_command(['systemctl', 'is-active', 'proxkube-daemon'],
                outfunc => sub { $daemon_status = $_[0]; });
        };
        chomp $daemon_status if $daemon_status;

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

        my @cmd;
        my $message;
        if ($action eq 'enable') {
            @cmd = ('systemctl', 'enable', '--now', 'proxkube-daemon');
            $message = 'Daemon enabled and started';
        } elsif ($action eq 'disable') {
            @cmd = ('systemctl', 'disable', '--now', 'proxkube-daemon');
            $message = 'Daemon disabled and stopped';
        } else {
            @cmd = ('systemctl', $action, 'proxkube-daemon');
            $message = "Daemon ${action}ed successfully";
        }

        my $output = '';
        eval {
            PVE::Tools::run_command(\@cmd,
                outfunc => sub { $output .= $_[0]; },
                errfunc => sub { $output .= $_[0]; });
        };
        my $err = $@;

        return {
            success => $err ? 0 : 1,
            message => $err ? "Failed: $output" : $message,
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
                pattern => '^[a-z0-9]([a-z0-9\\-]*[a-z0-9])?$',
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
        my $desc    = $param->{description} // "Managed by proxkube - Name: $name";

        # Escape description for safe YAML embedding.
        $desc =~ s/\\/\\\\/g;
        $desc =~ s/"/\\"/g;
        $desc =~ s/\n/\\n/g;

        # Build tags — always include "proxkube".
        my @tags = ('proxkube');
        if ($param->{tags}) {
            push @tags, split(/;/, $param->{tags});
        }

        # Generate a pod YAML manifest and apply it via proxkube CLI.
        my $yaml = "apiVersion: proxkube/v1\n";
        $yaml .= "kind: Pod\n";
        $yaml .= "metadata:\n";
        $yaml .= "  name: $name\n";
        $yaml .= "spec:\n";
        $yaml .= "  node: $node\n";
        $yaml .= "  image: $image\n";
        $yaml .= "  resources:\n";
        $yaml .= "    cpu: $cpu\n";
        $yaml .= "    memory: $memory\n";
        $yaml .= "    disk: $disk\n";
        $yaml .= "    storage: $storage\n";
        $yaml .= "    network:\n";
        $yaml .= "      bridge: $bridge\n";
        $yaml .= "      ip: dhcp\n";
        $yaml .= "  tags:\n";
        foreach my $tag (@tags) {
            $tag =~ s/[^a-zA-Z0-9\-_=]//g;
            $yaml .= "    - $tag\n" if $tag;
        }
        $yaml .= "  pool: $pool\n" if $pool;
        $yaml .= "  description: \"$desc\"\n" if $desc;

        # Write temporary manifest using a safe path independent of user input.
        my $tmpfile = "/tmp/proxkube-pod-$$-" . int(rand(100000)) . ".yaml";
        PVE::Tools::file_set_contents($tmpfile, $yaml);

        # Find proxkube binary.
        my $proxkube_bin = '/usr/bin/proxkube';
        $proxkube_bin = '/usr/local/bin/proxkube' unless -x $proxkube_bin;

        # Apply the manifest.
        my $output = '';
        eval {
            PVE::Tools::run_command([$proxkube_bin, 'apply', '-f', $tmpfile],
                outfunc => sub { $output .= $_[0]; },
                errfunc => sub { $output .= $_[0]; });
        };
        my $err = $@;
        unlink $tmpfile;

        die "Failed to create pod: $output\n" if $err;

        # Extract VMID from output (format: "pod/name applied (VMID NNN, phase Running)")
        my $vmid = 0;
        if ($output =~ /VMID\s+(\d+)/) {
            $vmid = int($1);
        }

        return {
            vmid => $vmid,
            name => $name,
        };
    },
});

1;
