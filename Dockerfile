# Compute wasm-shim sha256 checksum
FROM alpine:3 as wasm-shim-checksum
COPY kuadrant-ratelimit-wasm /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm
RUN sha256sum /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm | awk '{print $1}' > /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm.sha256
RUN cat /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm.sha256

# Build the manager binary
FROM golang:1.21 as builder

# Copy the wasm-shim
COPY --from=wasm-shim-checksum /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm.sha256 /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm.sha256

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/

# Build
RUN WASM_SHIM_SHA256=$(cat /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm.sha256) \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm.WASM_SHIM_SHA256=${WASM_SHIM_SHA256}" \
    -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=wasm-shim-checksum /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm /opt/kuadrant/wasm-shim/kuadrant-ratelimit-wasm
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
