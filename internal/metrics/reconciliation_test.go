//go:build unit

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func collectHistogramVec(h *prometheus.HistogramVec, label string) (*dto.Metric, error) {
	metric := &dto.Metric{}
	observer := h.WithLabelValues(label)
	err := observer.(prometheus.Metric).Write(metric)
	return metric, err
}

func TestObserveReconciliationDuration(t *testing.T) {
	reconciliationDuration.Reset()

	start := time.Now().Add(-100 * time.Millisecond)
	ObserveReconciliationDuration("data_plane", start)

	metric, err := collectHistogramVec(reconciliationDuration, "data_plane")
	if err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Histogram == nil {
		t.Fatal("expected histogram metric, got nil")
	}
	if *metric.Histogram.SampleCount != 1 {
		t.Errorf("expected 1 sample, got %d", *metric.Histogram.SampleCount)
	}
	if *metric.Histogram.SampleSum <= 0 {
		t.Errorf("expected positive sum, got %v", *metric.Histogram.SampleSum)
	}
}

func TestObserveEffectivePolicyDuration(t *testing.T) {
	effectivePolicyDuration.Reset()

	start := time.Now().Add(-50 * time.Millisecond)
	ObserveEffectivePolicyDuration("auth", start)

	metric, err := collectHistogramVec(effectivePolicyDuration, "auth")
	if err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if *metric.Histogram.SampleCount != 1 {
		t.Errorf("expected 1 sample, got %d", *metric.Histogram.SampleCount)
	}
}

func TestObserveTopologyRebuildDuration(t *testing.T) {
	metric := &dto.Metric{}
	ObserveTopologyRebuildDuration(time.Now().Add(-10 * time.Millisecond))

	if err := topologyRebuildDuration.(prometheus.Metric).Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Histogram == nil || *metric.Histogram.SampleCount < 1 {
		t.Error("expected at least 1 sample")
	}
}

func TestObserveAuthconfigGenerationDuration(t *testing.T) {
	metric := &dto.Metric{}
	ObserveAuthconfigGenerationDuration(time.Now().Add(-5 * time.Millisecond))

	if err := authconfigGenerationDuration.(prometheus.Metric).Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Histogram == nil || *metric.Histogram.SampleCount < 1 {
		t.Error("expected at least 1 sample")
	}
}

func TestObserveLimitadorLimitsGenerationDuration(t *testing.T) {
	metric := &dto.Metric{}
	ObserveLimitadorLimitsGenerationDuration(time.Now().Add(-5 * time.Millisecond))

	if err := limitadorLimitsGenerationDuration.(prometheus.Metric).Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Histogram == nil || *metric.Histogram.SampleCount < 1 {
		t.Error("expected at least 1 sample")
	}
}

func TestSetTopologyObjects(t *testing.T) {
	topologyObjects.Reset()

	SetTopologyObjects("Gateway", 3)
	SetTopologyObjects("HTTPRoute", 10)

	metric := &dto.Metric{}
	if err := topologyObjects.WithLabelValues("Gateway").Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if *metric.Gauge.Value != 3 {
		t.Errorf("expected 3, got %v", *metric.Gauge.Value)
	}

	if err := topologyObjects.WithLabelValues("HTTPRoute").Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if *metric.Gauge.Value != 10 {
		t.Errorf("expected 10, got %v", *metric.Gauge.Value)
	}
}

func TestResetTopologyObjects(t *testing.T) {
	topologyObjects.Reset()

	SetTopologyObjects("Gateway", 5)
	ResetTopologyObjects()

	metric := &dto.Metric{}
	if err := topologyObjects.WithLabelValues("Gateway").Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if *metric.Gauge.Value != 0 {
		t.Errorf("expected 0 after reset, got %v", *metric.Gauge.Value)
	}
}

func TestSetAuthconfigsGenerated(t *testing.T) {
	SetAuthconfigsGenerated(42)

	metric := &dto.Metric{}
	if err := authconfigsGenerated.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if *metric.Gauge.Value != 42 {
		t.Errorf("expected 42, got %v", *metric.Gauge.Value)
	}
}

func TestReconciliationBuckets(t *testing.T) {
	expected := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}
	if len(reconciliationBuckets) != len(expected) {
		t.Fatalf("expected %d buckets, got %d", len(expected), len(reconciliationBuckets))
	}
	for i, v := range expected {
		if reconciliationBuckets[i] != v {
			t.Errorf("bucket[%d]: expected %v, got %v", i, v, reconciliationBuckets[i])
		}
	}
}
