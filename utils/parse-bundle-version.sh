#!/usr/bin/env bash

set -euo pipefail

OPM=$1
YQ=$2
BUNDLE_IMAGE=$3

$OPM render $BUNDLE_IMAGE | $YQ eval '.properties[] | select(.type == "olm.package") | .value.version' -
