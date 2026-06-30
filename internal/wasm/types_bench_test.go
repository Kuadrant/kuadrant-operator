//go:build unit

package wasm

import (
	"fmt"
	"math/rand"
	"testing"

	"k8s.io/utils/ptr"
)

// buildScaleConfig creates a Config with numActionSets ActionSets, each containing
// actionsPerSet Actions with conditionalData entries. This mirrors the real-world
// scenario where each HTTPRoute+Policy pair produces an ActionSet.
func buildScaleConfig(numActionSets, actionsPerSet int) *Config {
	services := map[string]Service{
		"ratelimit-service": {
			Endpoint:    "kuadrant-ratelimit-service",
			Type:        RateLimitServiceType,
			FailureMode: FailureModeAllow,
			Timeout:     ptr.To("100ms"),
		},
		"auth-service": {
			Endpoint:    "kuadrant-auth-service",
			Type:        AuthServiceType,
			FailureMode: FailureModeDeny,
			Timeout:     ptr.To("200ms"),
		},
	}

	actionSets := make([]ActionSet, numActionSets)
	for i := range numActionSets {
		actions := make([]Action, actionsPerSet)
		for j := range actionsPerSet {
			actions[j] = Action{
				ServiceName: "ratelimit-service",
				Scope:       fmt.Sprintf("default/route-%d", i),
				Predicates:  []string{fmt.Sprintf("request.url_path.startsWith('/path-%d')", j)},
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{"source.address != '127.0.0.1'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   fmt.Sprintf("limit.route_%d_action_%d", i, j),
										Value: "1",
									},
								},
							},
						},
					},
					{
						Data: []DataType{
							{
								Value: &Expression{
									ExpressionItem: ExpressionItem{
										Key:   fmt.Sprintf("descriptor.route_%d_action_%d", i, j),
										Value: "request.host",
									},
								},
							},
						},
					},
				},
				SourcePolicyLocators: []string{
					fmt.Sprintf("RateLimitPolicy/default/rlp-%d", i),
				},
			}
		}

		actionSets[i] = ActionSet{
			Name: fmt.Sprintf("actionset-%d", i),
			RouteRuleConditions: RouteRuleConditions{
				Hostnames:  []string{fmt.Sprintf("app-%d.example.com", i)},
				Predicates: []string{"request.url_path.startsWith('/')"},
			},
			Actions: actions,
		}
	}

	return &Config{
		Services:   services,
		ActionSets: actionSets,
		RequestData: map[string]string{
			"metrics.labels.user": "auth.identity.user",
		},
	}
}

// shuffleActionSets returns a copy of the config with ActionSets in a different order.
// This simulates the non-determinism that occurs when ActionSets are built from Go
// map iteration, which is the root cause of Issue #1934.
func shuffleActionSets(cfg *Config, seed int64) *Config {
	shuffled := make([]ActionSet, len(cfg.ActionSets))
	copy(shuffled, cfg.ActionSets)

	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return &Config{
		Services:          cfg.Services,
		ActionSets:        shuffled,
		RequestData:       cfg.RequestData,
		DescriptorService: cfg.DescriptorService,
		Observability:     cfg.Observability,
	}
}

// shuffleActions returns a copy of the config with Actions within each ActionSet shuffled.
func shuffleActions(cfg *Config, seed int64) *Config {
	rng := rand.New(rand.NewSource(seed))

	newActionSets := make([]ActionSet, len(cfg.ActionSets))
	for i, as := range cfg.ActionSets {
		actions := make([]Action, len(as.Actions))
		copy(actions, as.Actions)
		rng.Shuffle(len(actions), func(a, b int) {
			actions[a], actions[b] = actions[b], actions[a]
		})
		newActionSets[i] = ActionSet{
			Name:                as.Name,
			RouteRuleConditions: as.RouteRuleConditions,
			Actions:             actions,
		}
	}

	return &Config{
		Services:          cfg.Services,
		ActionSets:        newActionSets,
		RequestData:       cfg.RequestData,
		DescriptorService: cfg.DescriptorService,
		Observability:     cfg.Observability,
	}
}

// TestEqualToNonDeterminism demonstrates the bug from Issue #1934:
// Two semantically identical configs that differ only in slice ordering
// are reported as not equal by the current EqualTo() implementation.
// This causes infinite reconciliation loops because Go map iteration
// produces different orderings on each run.
func TestEqualToNonDeterminism(t *testing.T) {
	testCases := []struct {
		name         string
		numActionSets int
	}{
		{"1_actionset", 1},
		{"10_actionsets", 10},
		{"50_actionsets", 50},
		{"100_actionsets", 100},
		{"300_actionsets", 300},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := buildScaleConfig(tc.numActionSets, 2)

			falseNegatives := 0
			trials := 100
			for seed := range trials {
				shuffled := shuffleActionSets(original, int64(seed))
				if !original.EqualTo(shuffled) {
					falseNegatives++
				}
			}

			if tc.numActionSets > 1 && falseNegatives == 0 {
				t.Logf("SURPRISING: all %d trials matched for %d action sets — order-sensitive comparison happened to match", trials, tc.numActionSets)
			}

			if falseNegatives > 0 {
				t.Logf("BUG CONFIRMED: %d/%d trials reported NOT equal for semantically identical configs with %d action sets (order-sensitive comparison)",
					falseNegatives, trials, tc.numActionSets)
				t.Logf("This causes the operator to detect phantom changes and re-reconcile infinitely (Issue #1934)")
			}
		})
	}
}

// TestEqualToActionOrderNonDeterminism demonstrates the same bug at the
// Action level within an ActionSet.
func TestEqualToActionOrderNonDeterminism(t *testing.T) {
	original := buildScaleConfig(10, 5)

	falseNegatives := 0
	trials := 100
	for seed := range trials {
		shuffled := shuffleActions(original, int64(seed))
		if !original.EqualTo(shuffled) {
			falseNegatives++
		}
	}

	if falseNegatives > 0 {
		t.Logf("BUG CONFIRMED: %d/%d trials reported NOT equal when Actions within ActionSets are reordered",
			falseNegatives, trials)
	}
}

// BenchmarkConfigEqualTo measures the cost of comparing two identical configs.
func BenchmarkConfigEqualTo(b *testing.B) {
	benchCases := []struct {
		numActionSets int
		actionsPerSet int
	}{
		{10, 2},
		{50, 2},
		{100, 2},
		{300, 2},
		{300, 5},
	}

	for _, bc := range benchCases {
		name := fmt.Sprintf("actionsets=%d_actions=%d", bc.numActionSets, bc.actionsPerSet)
		cfg1 := buildScaleConfig(bc.numActionSets, bc.actionsPerSet)
		cfg2 := buildScaleConfig(bc.numActionSets, bc.actionsPerSet)

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				cfg1.EqualTo(cfg2)
			}
		})
	}
}

// BenchmarkConfigEqualToShuffled measures the cost of comparing two semantically
// identical configs where the ActionSets are in different order. In the current
// implementation this always returns false (the bug), but we benchmark it to show
// the comparison cost that happens on every reconciliation cycle.
func BenchmarkConfigEqualToShuffled(b *testing.B) {
	benchCases := []struct {
		numActionSets int
	}{
		{10},
		{50},
		{100},
		{300},
	}

	for _, bc := range benchCases {
		name := fmt.Sprintf("actionsets=%d_shuffled", bc.numActionSets)
		cfg1 := buildScaleConfig(bc.numActionSets, 2)
		cfg2 := shuffleActionSets(cfg1, 42)

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				cfg1.EqualTo(cfg2)
			}
		})
	}
}
