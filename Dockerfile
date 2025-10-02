# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/

# Set environment variables for cross-compilation
ARG TARGETARCH

# Build

ARG GIT_SHA
ARG DIRTY
ARG VERSION
ARG WITH_EXTENSIONS=false

ENV GIT_SHA=${GIT_SHA:-unknown}
ENV DIRTY=${DIRTY:-unknown}
ENV VERSION=${VERSION:-unknown}

# Kuadrant Operator
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -ldflags "-X main.version=${VERSION} -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" -o manager cmd/main.go

# Conditionally build extensions
RUN mkdir -p extensions
RUN if [ "$WITH_EXTENSIONS" = "true" ]; then \
    echo "Building extensions..." && \
    mkdir -p extensions/oidc-policy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -o extensions/oidc-policy/oidc-policy cmd/extensions/oidc-policy/main.go && \
    mkdir -p extensions/plan-policy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -o extensions/plan-policy/plan-policy cmd/extensions/plan-policy/main.go && \
    mkdir -p extensions/telemetry-policy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -o extensions/telemetry-policy/telemetry-policy cmd/extensions/telemetry-policy/main.go; \
    else \
    echo "Skipping extensions build"; \
    fi

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/extensions /extensions

# Quay image expiry
ARG QUAY_IMAGE_EXPIRY
ENV QUAY_IMAGE_EXPIRY=${QUAY_IMAGE_EXPIRY:-never}
LABEL quay.expires-after=$QUAY_IMAGE_EXPIRY

ENTRYPOINT ["/manager"]
