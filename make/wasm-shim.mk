##@ Wasm shim targets

WASM_SHIM = $(PROJECT_PATH)/kuadrant-ratelimit-wasm
WASM_SHIM_VERSION = v0.4.0-alpha.1
$(WASM_SHIM):
	@{ \
	ASSET_ID=$$(curl -s -H "Accept: application/vnd.github.v3.raw" \
		https://api.github.com/repos/Kuadrant/wasm-shim/releases | \
		jq ". | map(select(.tag_name == \"$(WASM_SHIM_VERSION)\"))[0].assets | map(select(.name == \"kuadrant-ratelimit-wasm-$(WASM_SHIM_VERSION)\"))[0].id" \
	) \
	&& echo "Downloading kuadrant-ratelimit-wasm@$(WASM_SHIM_VERSION) from https://api.github.com/repos/Kuadrant/wasm-shim/releases/assets/$${ASSET_ID}" \
	&& curl -sSLo $@ -H "Accept: application/octet-stream" https://api.github.com/repos/Kuadrant/wasm-shim/releases/assets/$${ASSET_ID}; \
	sha256sum $@; \
	}

.PHONY: wasm-shim
wasm-shim: $(WASM_SHIM) ## Download opm locally if necessary.
