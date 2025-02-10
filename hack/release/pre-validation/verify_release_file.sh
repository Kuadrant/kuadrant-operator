#!/usr/bin/env bash

echo "Verifing the release file"
file=$env/release.toml
if [[ -f $file ]]; then
	echo "Release file exists"
else
	echo "Release file could not be found in $env"
	exit 1
fi

has_error=0

check_field() {
    v=$(yq --output-format=yaml --unwrapScalar "$1" "$file" 2>/dev/null)
    if [[ -z "$v" || "$v" == "null" ]]; then
        echo "$2 is a required field. Please update."
        has_error=1
    fi
}

check_field ".kuadrant.olm.channels" "kuadrant.olm.channels"
check_field ".kuadrant.olm.default_channel" "kuadrant.olm.default_channel"
check_field ".kuadrant.release" "kuadrant.release"
check_field ".dependencies.Authorino" "dependencies.Authorino"
check_field ".dependencies.Console_plugin" "dependencies.Console_plugin"
check_field ".dependencies.DNS" "dependencies.DNS"
check_field ".dependencies.Limitador" "dependencies.Limitador"
check_field ".dependencies.Wasm_shim" "dependencies.Wasm_shim"

exit $has_error
