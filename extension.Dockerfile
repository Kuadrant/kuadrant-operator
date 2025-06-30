# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.23 AS builder

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

ENV GIT_SHA=${GIT_SHA:-unknown}
ENV DIRTY=${DIRTY:-unknown}
ENV VERSION=${VERSION:-unknown}

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -ldflags "-X main.version=${VERSION} -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" -o manager cmd/main.go


RUN mkdir -p extensions/plan-policy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -o extensions/plan-policy/plan-policy cmd/extensions/plan-policy/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder --chown=65532:65532 /workspace/extensions /extensions
USER 65532:65532

# Quay image expiry
ARG QUAY_IMAGE_EXPIRY
ENV QUAY_IMAGE_EXPIRY=${QUAY_IMAGE_EXPIRY:-never}
LABEL quay.expires-after=$QUAY_IMAGE_EXPIRY

ENTRYPOINT ["/manager"]
