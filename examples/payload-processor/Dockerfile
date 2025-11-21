FROM golang:1.25 AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

WORKDIR /build

# Copy go module files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the shared library
RUN go build -buildmode=c-shared -o extract_model.so main.go

# Verify the build
RUN ls -lh extract_model.so && file extract_model.so

# Runtime image that just contains the .so file
FROM scratch
COPY --from=builder /build/extract_model.so /extract_model.so
