#!/bin/bash

# Generates a MetalLB IpAddressPool for the given docker network.
# https://metallb.org/configuration/#defining-the-ips-to-assign-to-the-load-balancer-services
#
# Example:
# ./utils/docker-network-ipaddresspool.sh kind | kubectl apply -n metallb-system -f -

set -euo pipefail

networkName=$1
YQ="${2:-yq}"

SUBNET=""
if command -v podman &> /dev/null; then
  SUBNET=$(podman network inspect kind | grep -Eo '"subnet": "[0-9.]+/[0-9]+' | awk -F\" '{print $4}' | head -n 1)
elif command -v docker &> /dev/null; then
  ## Parse kind network subnet
  ## Take only IPv4 subnets, exclude IPv6
  SUBNET=$(docker network inspect $networkName --format '{{json .IPAM.Config }}' | \
      ${YQ} '.[] | select( .Subnet | test("^((25[0-5]|(2[0-4]|1\d|[1-9]|)\d)\.?\b){4}/\d+$")) | .Subnet')
fi

if [[ -z "$SUBNET" ]]; then
   echo "Error: parsing IPv4 network address for '$networkName' docker network"
   exit 1
fi

# shellcheck disable=SC2206
subnetParts=(${SUBNET//./ })
cidr="${subnetParts[0]}.${subnetParts[1]}.0.252/30"

cat <<EOF | ADDRESS=$cidr ${YQ} '(select(.kind == "IPAddressPool") | .spec.addresses[0]) = env(ADDRESS)'
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kuadrant-local
spec:
  addresses: [] # set by make target
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
EOF
