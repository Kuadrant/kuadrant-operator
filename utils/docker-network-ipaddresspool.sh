#!/bin/bash

# Generates a MetalLB IpAddressPool for the given docker network.
# https://metallb.org/configuration/#defining-the-ips-to-assign-to-the-load-balancer-services
#
# Example:
# ./utils/docker-network-ipaddresspool.sh kind | kubectl apply -n metallb-system -f -

set -euo pipefail

networkName=$1
yq=$2

## Parse kind network subnet
## Take only IPv4 subnets, exclude IPv6
SUBNET=$(docker network inspect $networkName --format '{{json .IPAM.Config }}' | \
    ${yq} '.[] | select( .Subnet | test("^((25[0-5]|(2[0-4]|1\d|[1-9]|)\d)\.?\b){4}/\d+$")) | .Subnet')

echo "---
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
  namespace: metallb-system
" | \
ADDRESS=$SUBNET ${yq} '(select(.kind == "IPAddressPool") | .spec.addresses[0]) = env(ADDRESS)'
