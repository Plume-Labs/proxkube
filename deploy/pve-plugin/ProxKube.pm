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
# containers (identified by the "proxkube" tag).
#
# Installation:
#   cp ProxKube.pm /usr/share/perl5/PVE/API2/
#   systemctl restart pvedaemon pveproxy

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

1;
