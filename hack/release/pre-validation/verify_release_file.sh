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

check_field ".kuadrant.channels" "kuadrant.channels"
check_field ".kuadrant.default_channel" "kuadrant.default_channel"
check_field ".kuadrant.release" "kuadrant.release"
check_field ".dependencies.Authorino_bundle" "dependencies.Authorino_bundle"
check_field ".dependencies.Console_plugin" "dependencies.Console_plugin"
check_field ".dependencies.DNS_bundle" "dependencies.DNS_bundle"
check_field ".dependencies.Limitador_bundle" "dependencies.Limitador_bundle"
check_field ".dependencies.Wasm_shim" "dependencies.Wasm_shim"

exit $has_error
