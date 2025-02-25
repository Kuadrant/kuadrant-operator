#!/usr/bin/env bash

echo "Verifing the release file"
file=$env/release.yaml
if [[ -f $file ]]; then
	echo "Release file exists"
else
	echo "Release file could not be found in $env"
	exit 1
fi

has_error=0

check_field() {
    v=$(yq --unwrapScalar "$1" "$file" 2>/dev/null)
    if [[ -z "$v" || "$v" == "null" ]]; then
        echo "$2 is a required field. Please update."
        has_error=1
    fi
}

check_field ".olm.channels" "olm.channels"
check_field ".olm.default-channel" "olm.default-channel"
check_field ".kuadrant.version" "kuadrant.version"
check_field ".dependencies.authorino-operator" "dependencies.authorino-operator"
check_field ".dependencies.console-plugin" "dependencies.console-plugin"
check_field ".dependencies.dns-operator" "dependencies.dns-operator"
check_field ".dependencies.limitador-operator" "dependencies.limitador-operator"
check_field ".dependencies.wasm-shim" "dependencies.wasm-shim"

exit $has_error
