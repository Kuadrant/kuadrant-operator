package v1

import (
	"github.com/samber/lo"
)

func NewPredicate(predicate string) Predicate {
	return Predicate{Predicate: predicate}
}

// Predicate defines one CEL expression that must be evaluated to bool
type Predicate struct {
	// +kubebuilder:validation:MinLength=1
	Predicate string `json:"predicate"`
}

func NewWhenPredicates(predicates ...string) WhenPredicates {
	whenPredicates := make(WhenPredicates, 0)
	for _, predicate := range predicates {
		whenPredicates = append(whenPredicates, NewPredicate(predicate))
	}

	return whenPredicates
}

type WhenPredicates []Predicate

func (w WhenPredicates) Extend(other WhenPredicates) WhenPredicates {
	return append(w, other...)
}

func (w WhenPredicates) Into() []string {
	if w == nil {
		return nil
	}

	return lo.Map(w, func(p Predicate, _ int) string { return p.Predicate })
}

type MergeableWhenPredicates struct {
	// Overall conditions for the policy to be enforced.
	// If omitted, the policy will be enforced at all requests to the protected routes.
	// If present, all conditions must match for the policy to be enforced.
	// +optional
	Predicates WhenPredicates `json:"when,omitempty"`

	// Source stores the locator of the policy where the limit is orignaly defined (internal use)
	Source string `json:"-"`
}

var _ MergeableRule = &MergeableWhenPredicates{}

func (p *MergeableWhenPredicates) GetSpec() any {
	return p.Predicates
}

func (p *MergeableWhenPredicates) GetSource() string {
	return p.Source
}

func (p *MergeableWhenPredicates) WithSource(source string) MergeableRule {
	p.Source = source
	return p
}
