.PHONY: build clean test

# Detect cluster architecture if not specified
ARCH ?= $(shell kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}' 2>/dev/null || echo "amd64")

# Container runtime (podman or docker)
CONTAINER_RUNTIME ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)

# Build using container (for cross-platform builds from macOS)
build:
	@echo "Building Envoy Golang filter for Linux/$(ARCH) using $(CONTAINER_RUNTIME)..."
	$(CONTAINER_RUNTIME) run --rm \
		-v $(PWD):/build:z \
		-w /build \
		--platform linux/$(ARCH) \
		golang:1.25 \
		sh -c "go mod download && go build -buildmode=c-shared -o extract_model.so main.go"
	@echo "Built: extract_model.so (Linux/$(ARCH))"

# Build locally (only works on Linux)
build-local:
	@echo "Building Envoy Golang filter shared library..."
	go mod download
	go build -buildmode=c-shared -o extract_model.so main.go
	@echo "Built: extract_model.so"

# Clean build artifacts
clean:
	rm -f extract_model.so extract_model.h

# Run tests (if any)
test:
	go test -v ./...

# Build with Docker (for consistent build environment)
docker-build:
	docker build -t payload-processor-builder -f Dockerfile .
	docker create --name temp-container payload-processor-builder
	docker cp temp-container:/build/extract_model.so ./extract_model.so
	docker rm temp-container
	@echo "Built: extract_model.so (via Docker)"

# Verify the shared library was built correctly
verify:
	@if [ -f extract_model.so ]; then \
		echo "✓ extract_model.so exists"; \
		file extract_model.so; \
		echo "Checking for ELF format..."; \
		if file extract_model.so | grep -q "ELF"; then \
			echo "✓ Correct format (ELF - Linux binary)"; \
		else \
			echo "✗ Wrong format - should be ELF, not Mach-O"; \
			echo "  Did you forget to run 'make clean && make build'?"; \
			exit 1; \
		fi; \
	else \
		echo "✗ extract_model.so not found"; \
		exit 1; \
	fi
