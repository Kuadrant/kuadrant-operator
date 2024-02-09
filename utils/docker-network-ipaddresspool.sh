#!/bin/bash

# Generates a MetalLB IpAddressPool for the given docker network.
# https://metallb.org/configuration/#defining-the-ips-to-assign-to-the-load-balancer-services
#
# Example:
# ./utils/docker-network-ipaddresspool.sh kind | kubectl apply -n metallb-system -f -

set -euo pipefail

networkName=$1
YQ="${2:-yq}"

subnet=`docker network inspect $networkName -f json | $YQ -r -o=json '.[].IPAM.Config.[] | select(.Gateway | test("^((25[0-5]|(2[0-4]|1\d|[1-9]|)\d)\.?\b){4}$")).Subnet'`
if [[ -z "$subnet" ]]; then
   echo "Error: parsing IPv4 network address for '$networkName' docker network"
   exit 1
fi

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
