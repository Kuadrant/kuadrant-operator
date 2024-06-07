##@ Alerts
export IMAGE ?= quay.io/prometheus/prometheus
export AVAILABILITY_SLO_RULES ?= $(PROJECT_PATH)/examples/alerts/slo-availability.yaml
export LATENCY_SLO_RULES ?= $(PROJECT_PATH)/examples/alerts/slo-latency.yaml
export UNIT_TEST_DIR ?= $(PROJECT_PATH)/examples/alerts/tests
export SLOTH = $(PROJECT_PATH)/bin/sloth
export ALERTS_SLOTH_INPUT_DIR = /examples/alerts/sloth
export ALERTS_SLOTH_OUTPUT_DIR = /examples/alerts


container-runtime-tool:
	$(eval CONTAINER_RUNTIME_BIN := $(shell if command -v docker &>/dev/null; then \
                                            echo "docker"; \
                                        elif command -v podman &>/dev/null; then \
                                            echo "podman"; \
                                        else \
                                            echo "Neither Docker nor Podman is installed. Exiting..."; \
                                            exit 1; \
                                        fi))

alerts-tests: container-runtime-tool ## Test alerts using promtool 
	$(CONTAINER_RUNTIME_BIN) run --rm -t \
	-v $(AVAILABILITY_SLO_RULES):/prometheus/slo-availability.yaml \
	-v $(LATENCY_SLO_RULES):/prometheus/slo-latency.yaml \
	-v $(UNIT_TEST_DIR):/prometheus/tests --entrypoint=/bin/sh \
	$(IMAGE) -c 'tail -n +16 slo-latency.yaml > latency-rules.yaml  && tail -n +16 slo-availability.yaml > availability-rules.yaml && cd tests && promtool test rules *'

sloth: $(SLOTH) ## Install Sloth
$(SLOTH):
	cd $(PROJECT_PATH)/bin && curl -L https://github.com/slok/sloth/releases/download/v0.11.0/sloth-$(OS)-$(ARCH) > sloth && chmod +x sloth
    
sloth-generate: sloth ## Generate alerts using Sloth templates
	$(SLOTH) generate -i $(PROJECT_PATH)$(ALERTS_SLOTH_INPUT_DIR) -o $(PROJECT_PATH)$(ALERTS_SLOTH_OUTPUT_DIR) --default-slo-period=28d
