package wasm

import (
	"fmt"
	"sort"
	"strings"
)

// ActionSpec is a service-agnostic intermediate that collects policy-derived data
// before materialization into a concrete TypedAction via Build().
type ActionSpec struct {
	ServiceName     string
	Scope           string
	Predicates      []string
	ConditionalData []ConditionalData
	Sources         []string
	Bindings        []DataBinding
}

type DataBinding struct {
	Domain     string
	Field      string
	Expression string
}

// ActionSetSpec is the intermediate between policy processing and final Config construction.
type ActionSetSpec struct {
	Name                string
	RouteRuleConditions RouteRuleConditions
	Specs               []ActionSpec
	ExtensionActions    []TypedAction
	SourceRoute         string
}

// HasAuthAccess checks whether any predicate or expression value references "auth."
func (s *ActionSpec) HasAuthAccess() bool {
	for _, cd := range s.ConditionalData {
		for _, predicate := range cd.Predicates {
			if strings.Contains(predicate, "auth.") {
				return true
			}
		}
		for _, data := range cd.Data {
			switch val := data.Value.(type) {
			case *Static:
				continue
			case *Expression:
				if strings.Contains(val.ExpressionItem.Value, "auth.") {
					return true
				}
			}
		}
	}
	return false
}

const (
	authResponseVar      = "auth_response"
	rateLimitResponseVar = "ratelimit_response"
	reportResponseVar    = "report_response"
)

// Build materializes this ActionSpec into a concrete TypedAction by dispatching on ServiceName.
func (s *ActionSpec) Build() TypedAction {
	switch s.ServiceName {
	case AuthServiceName:
		return s.buildAuth()
	case RateLimitServiceName, RateLimitCheckServiceName:
		return s.buildRateLimit(rateLimitResponseVar, true, "ratelimit")
	case RateLimitReportServiceName:
		return s.buildRateLimit(reportResponseVar, false, "ratelimit_report")
	default:
		return NewFailAction("true", fmt.Sprintf("unknown service: %s", s.ServiceName)).
			WithSources(s.Sources)
	}
}

func (s *ActionSpec) buildAuth() *GrpcAction {
	bindings := filterBindings(s.Bindings, false)
	request := CheckRequestCEL{
		Scope:           s.Scope,
		MetadataContext: buildMetadataContext(bindings),
	}
	predicate := buildActionPredicate(s.Predicates)

	return NewGrpcAction(predicate, authResponseVar, s.ServiceName, request.ToCEL(), "auth").
		WithSources(s.Sources).
		WithOnReply(buildAuthOnReply(authResponseVar)...)
}

func (s *ActionSpec) buildRateLimit(responseVar string, isGuard bool, label string) *GrpcAction {
	includeAll := !isGuard
	bindings := filterBindings(s.Bindings, includeAll)
	request := buildRateLimitRequest(s.Scope, s.ConditionalData, bindings)
	predicate := buildRateLimitPredicate(s.Predicates, s.ConditionalData)

	var onReply []TypedAction
	if isGuard {
		onReply = buildRateLimitOnReply(responseVar)
	} else {
		onReply = buildReportOnReply(responseVar)
	}

	return NewGrpcAction(predicate, responseVar, s.ServiceName, request.ToCEL(), label).
		WithGuard(isGuard).
		WithSources(s.Sources).
		WithOnReply(onReply...)
}

// --- Predicate helpers ---

func buildActionPredicate(predicates []string) string {
	return joinPredicates(predicates, "&&")
}

func buildDescriptorPredicate(conditionalData []ConditionalData) string {
	if len(conditionalData) == 0 {
		return "true"
	}
	for _, cd := range conditionalData {
		if len(cd.Predicates) == 0 {
			return "true"
		}
	}

	var blockPredicates []string
	for _, cd := range conditionalData {
		if len(cd.Predicates) == 1 {
			blockPredicates = append(blockPredicates, cd.Predicates[0])
		} else {
			blockPredicates = append(blockPredicates, fmt.Sprintf("(%s)", joinPredicates(cd.Predicates, "&&")))
		}
	}

	if len(blockPredicates) == 1 {
		return blockPredicates[0]
	}
	return strings.Join(blockPredicates, " || ")
}

func buildRateLimitPredicate(actionPredicates []string, conditionalData []ConditionalData) string {
	actionPred := buildActionPredicate(actionPredicates)
	conditionalPred := buildDescriptorPredicate(conditionalData)

	if actionPred == "true" && conditionalPred == "true" {
		return "true"
	}
	if actionPred == "true" {
		return conditionalPred
	}
	if conditionalPred == "true" {
		return actionPred
	}
	return fmt.Sprintf("(%s) && (%s)", actionPred, conditionalPred)
}

// --- Binding helpers ---

// domainAndFieldName splits a binding key on the last "." into (domain, field).
// "auth.identity.user" → ("auth.identity", "user")
// "simple" → ("", "simple")
func domainAndFieldName(name string) (string, string) {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}

func filterBindings(bindings []DataBinding, includeAll bool) []DataBinding {
	if includeAll {
		return bindings
	}
	var filtered []DataBinding
	for _, b := range bindings {
		if !strings.Contains(b.Expression, "responseBodyJSON(") {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// --- Auth message construction ---

func buildMetadataContext(bindings []DataBinding) MetadataCEL {
	if len(bindings) == 0 {
		return MetadataCEL{}
	}

	type domainGroup struct {
		domain string
		fields []MetadataFieldCEL
	}
	byDomain := make(map[string]*domainGroup)
	for _, b := range bindings {
		key := "io.kuadrant"
		if b.Domain != "" {
			key = fmt.Sprintf("io.kuadrant.%s", b.Domain)
		}
		if _, ok := byDomain[key]; !ok {
			byDomain[key] = &domainGroup{domain: key}
		}
		byDomain[key].fields = append(byDomain[key].fields, MetadataFieldCEL{
			Key:        b.Field,
			Expression: b.Expression,
		})
	}

	groups := make([]domainGroup, 0, len(byDomain))
	for _, g := range byDomain {
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].domain < groups[j].domain })

	entries := make([]FilterMetadataEntryCEL, len(groups))
	for i, g := range groups {
		entries[i] = FilterMetadataEntryCEL{
			Domain: g.domain,
			Fields: g.fields,
		}
	}
	return MetadataCEL{FilterMetadata: entries}
}

func buildAuthOnReply(name string) []TypedAction {
	return []TypedAction{
		NewDenyAction(
			fmt.Sprintf("has(%s.denied_response)", name),
			fmt.Sprintf(
				`DenyResponse{status: (%s.denied_response.status.code != 0) ? uint(%s.denied_response.status.code) : 403u, headers: %s.denied_response.headers, body: %s.denied_response.body}`,
				name, name, name, name,
			),
		),
		NewFailAction(
			fmt.Sprintf(
				"has(%s.ok_response) && (%s.ok_response.response_headers_to_add.size() > 0 || %s.ok_response.headers_to_remove.size() > 0 || %s.ok_response.query_parameters_to_set.size() > 0 || %s.ok_response.query_parameters_to_remove.size() > 0)",
				name, name, name, name, name,
			),
			"Unsupported field in OkHttpResponse",
		),
		NewStoreAction(
			fmt.Sprintf("has(%s.ok_response) && has(%s.dynamic_metadata)", name, name),
			"auth",
			fmt.Sprintf("%s.dynamic_metadata", name),
		).WithExportToHost(true),
		NewHeadersAction(
			fmt.Sprintf("has(%s.ok_response)", name),
			"request",
			fmt.Sprintf("%s.ok_response.headers", name),
		),
		NewFailAction(
			fmt.Sprintf("!has(%s.denied_response) && !has(%s.ok_response)", name, name),
			fmt.Sprintf("Auth response contained no http_response from %s", name),
		),
	}
}

// --- RateLimit message construction ---

var rateLimitKnownAttrs = [2]string{"ratelimit.domain", "ratelimit.hits_addend"}

func isRateLimitKnownAttr(data DataType) bool {
	var key string
	switch val := data.Value.(type) {
	case *Static:
		key = val.Static.Key
	case *Expression:
		key = val.ExpressionItem.Key
	}
	for _, attr := range rateLimitKnownAttrs {
		if key == attr {
			return true
		}
	}
	return false
}

func findRateLimitKnownAttrCEL(conditionalData []ConditionalData, attrKey string) string {
	for _, cd := range conditionalData {
		for _, item := range cd.Data {
			switch val := item.Value.(type) {
			case *Static:
				if val.Static.Key == attrKey {
					if attrKey == "ratelimit.hits_addend" {
						return fmt.Sprintf("uint(%s)", val.Static.Value)
					}
					return fmt.Sprintf(`"%s"`, escapeCELString(val.Static.Value))
				}
			case *Expression:
				if val.ExpressionItem.Key == attrKey {
					if attrKey == "ratelimit.hits_addend" {
						return fmt.Sprintf("uint(%s)", val.ExpressionItem.Value)
					}
					return val.ExpressionItem.Value
				}
			}
		}
	}
	return ""
}

func buildRateLimitRequest(scope string, conditionalData []ConditionalData, bindings []DataBinding) RateLimitRequestCEL {
	domain := findRateLimitKnownAttrCEL(conditionalData, "ratelimit.domain")
	if domain == "" {
		domain = fmt.Sprintf(`"%s"`, escapeCELString(scope))
	}

	hitsAddend := findRateLimitKnownAttrCEL(conditionalData, "ratelimit.hits_addend")
	if hitsAddend == "" {
		hitsAddend = "1u"
	}

	var descriptors []RateLimitDescriptorCEL

	if desc := conditionalDataToDescriptor(conditionalData); desc != nil {
		descriptors = append(descriptors, *desc)
	}

	if bindingDesc := bindingsToDescriptor(bindings); bindingDesc != nil {
		descriptors = append(descriptors, *bindingDesc)
	}

	return RateLimitRequestCEL{
		Domain:      domain,
		HitsAddend:  hitsAddend,
		Descriptors: descriptors,
	}
}

func conditionalDataToDescriptor(conditionalData []ConditionalData) *RateLimitDescriptorCEL {
	var unconditionalEntries []DescriptorEntryCEL
	var entryListParts []string

	for _, cd := range conditionalData {
		var entries []DescriptorEntryCEL
		for _, item := range cd.Data {
			if isRateLimitKnownAttr(item) {
				continue
			}
			entries = append(entries, dataTypeToDescriptorEntry(item))
		}
		if len(entries) == 0 {
			continue
		}

		if len(cd.Predicates) == 0 {
			unconditionalEntries = append(unconditionalEntries, entries...)
		} else {
			predicate := joinPredicates(cd.Predicates, "&&")
			part := RateLimitDescriptorCEL{Entries: entries, Predicate: predicate}
			entryListParts = append(entryListParts, part.conditionalEntriesCEL())
		}
	}

	if len(unconditionalEntries) == 0 && len(entryListParts) == 0 {
		return nil
	}

	if len(unconditionalEntries) > 0 {
		strs := make([]string, len(unconditionalEntries))
		for i, e := range unconditionalEntries {
			strs[i] = e.ToCEL()
		}
		entryListParts = append(entryListParts, fmt.Sprintf("[%s]", strings.Join(strs, ", ")))
	}

	if len(entryListParts) == 1 && len(unconditionalEntries) > 0 {
		return &RateLimitDescriptorCEL{Entries: unconditionalEntries}
	}

	return &RateLimitDescriptorCEL{EntriesExpr: strings.Join(entryListParts, " + ")}
}

func dataTypeToDescriptorEntry(data DataType) DescriptorEntryCEL {
	switch val := data.Value.(type) {
	case *Static:
		return DescriptorEntryCEL{
			Key:      val.Static.Key,
			ValueCEL: fmt.Sprintf(`"%s"`, escapeCELString(val.Static.Value)),
		}
	case *Expression:
		return DescriptorEntryCEL{
			Key:      val.ExpressionItem.Key,
			ValueCEL: fmt.Sprintf("string(%s)", val.ExpressionItem.Value),
		}
	default:
		return DescriptorEntryCEL{}
	}
}

func bindingsToDescriptor(bindings []DataBinding) *RateLimitDescriptorCEL {
	if len(bindings) == 0 {
		return nil
	}
	entries := make([]DescriptorEntryCEL, 0, len(bindings))
	for _, b := range bindings {
		key := b.Field
		if b.Domain != "" && b.Domain != "metrics.labels" {
			key = fmt.Sprintf("%s.%s", b.Domain, b.Field)
		}
		entries = append(entries, DescriptorEntryCEL{
			Key:      key,
			ValueCEL: fmt.Sprintf("string(%s)", b.Expression),
		})
	}
	return &RateLimitDescriptorCEL{Entries: entries}
}

// --- RateLimit on_reply ---

func buildRateLimitOnReply(name string) []TypedAction {
	return []TypedAction{
		NewDenyAction(
			fmt.Sprintf("%s.overall_code == 2", name),
			fmt.Sprintf(
				`DenyResponse{status: 429u, headers: %s.response_headers_to_add, body: "Too Many Requests\n"}`,
				name,
			),
		),
		NewHeadersAction(
			fmt.Sprintf("%s.overall_code == 1", name),
			"response",
			fmt.Sprintf("%s.response_headers_to_add", name),
		),
		NewFailAction(
			fmt.Sprintf("%s.overall_code != 1 && %s.overall_code != 2", name, name),
			fmt.Sprintf("Unknown rate limit response code from %s", name),
		),
	}
}

func buildReportOnReply(name string) []TypedAction {
	return []TypedAction{
		NewFailAction(
			fmt.Sprintf("!has(%s.overall_code)", name),
			"Rate limit report failed: invalid gRPC response",
		).WithTerminal(false).WithGuard(false),
	}
}
