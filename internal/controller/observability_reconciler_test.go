package controllers

import (
	"strings"
	"testing"
)

func TestIstioPodMonitorBuild_RegexEscaping(t *testing.T) {
	pm := istioPodMonitorBuild("test-ns")
	if len(pm.Spec.PodMetricsEndpoints) == 0 {
		t.Fatal("expected at least one pod metrics endpoint")
	}

	var addressRegexes int
	for _, rc := range pm.Spec.PodMetricsEndpoints[0].RelabelConfigs {
		if rc.TargetLabel != "__address__" {
			continue
		}
		addressRegexes++
		if strings.Contains(rc.Regex, `\\d`) {
			t.Errorf("regex %q contains double-escaped \\d in raw string", rc.Regex)
		}
	}
	if addressRegexes != 2 {
		t.Errorf("expected 2 __address__ relabel regexes, got %d", addressRegexes)
	}
}
