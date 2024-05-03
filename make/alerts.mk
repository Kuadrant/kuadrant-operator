export WORKDIR ?= $(shell pwd)
export IMAGE ?= quay.io/prometheus/prometheus
export AVAILABILITY_SLO_RULES ?= ${WORKDIR}/examples/alerts/slo-availability.yaml
export LATENCY_SLO_RULES ?= ${WORKDIR}/examples/alerts/slo-latency.yaml
export UNIT_TEST_DIR ?= ${WORKDIR}/examples/alerts/tests



container-runtime-tool:
	$(eval CONTAINER_RUNTIME_BIN := $(shell if command -v docker &>/dev/null; then \
                                            echo "docker"; \
                                        elif command -v podman &>/dev/null; then \
                                            echo "podman"; \
                                        else \
                                            echo "Neither Docker nor Podman is installed. Exiting..."; \
                                            exit 1; \
                                        fi))


alerts-tests: container-runtime-tool
	$(CONTAINER_RUNTIME_BIN) run --rm -t \
	-v $(AVAILABILITY_SLO_RULES):/prometheus/slo-availability.yaml \
	-v $(LATENCY_SLO_RULES):/prometheus/slo-latency.yaml \
    -v $(UNIT_TEST_DIR):/prometheus/tests --entrypoint=/bin/sh \
$(IMAGE) -c 'tail -n +7 slo-latency.yaml > latency-rules.yaml  && tail -n +7 slo-availability.yaml > availability-rules.yaml && cd tests && promtool test rules *'
