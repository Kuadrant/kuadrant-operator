#!/bin/bash

# Generates a MetalLB IpAddressPool for the given docker network.
# https://metallb.org/configuration/#defining-the-ips-to-assign-to-the-load-balancer-services
#
# Example:
# ./utils/docker-network-ipaddresspool.sh kind | kubectl apply -n metallb-system -f -

set -euo pipefail

networkName=$1

subnet=`docker network inspect $networkName -f '{{ (index .IPAM.Config 0).Subnet }}'`
# shellcheck disable=SC2206
subnetParts=(${subnet//./ })
cidr="${subnetParts[0]}.${subnetParts[1]}.200.0/24"

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
