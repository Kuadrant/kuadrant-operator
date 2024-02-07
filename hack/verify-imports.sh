#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

make -C "$( dirname "${BASH_SOURCE[0]}")/../" imports
if ! git diff --quiet --exit-code ; then
	cat << EOF
ERROR: This check enforces that import statements are ordered correctly.
ERROR: The import statements are out of order. Run the following command
ERROR: to regenerate the statements:
ERROR: $ make imports
ERROR: The following differences were found:
EOF
	git diff
	exit 1
fi
