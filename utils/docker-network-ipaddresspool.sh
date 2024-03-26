#!/bin/bash

# Generates a MetalLB IpAddressPool for the given docker network.
# https://metallb.org/configuration/#defining-the-ips-to-assign-to-the-load-balancer-services
#
# Example:
# ./utils/docker-network-ipaddresspool.sh kind | kubectl apply -n metallb-system -f -

set -euo pipefail

networkName=$1

subnet=""
if command -v podman &> /dev/null; then
  subnet=`podman network inspect -f '{{range .Subnets}}{{if eq (len .Subnet.IP) 4}}{{.Subnet}}{{end}}{{end}}' kind`
elif command -v docker &> /dev/null; then
  subnet=`docker network inspect $networkName -f '{{ (index .IPAM.Config 0).Subnet }}'`
fi

if [[ -z "$subnet" ]]; then
   echo "Error: parsing IPv4 network address for '$networkName' docker network"
   exit 1
fi

# shellcheck disable=SC2206
subnetParts=(${subnet//./ })
cidr="${subnetParts[0]}.${subnetParts[1]}.0.252/30"

cat <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kuadrant-local
spec:
  addresses:
  - $cidr
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
EOF
