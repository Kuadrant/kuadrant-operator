package wasm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ActionSpec is a service-agnostic intermediate that collects policy-derived data
// before materialization into a concrete Action via Build().
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
	ExtensionActions    []Action
	SourceRoute         string
}

// HasAuthAccess checks whether any predicate or expression value references "auth."
func (s ActionSpec) HasAuthAccess() bool {
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

	AuthStorePath           = "auth"
	RateLimitCompleteSignal = "kuadrant.internal.ratelimit.complete"
)

// IsGuard returns true if this spec produces a guard action (runs during request phase).
func (s ActionSpec) IsGuard() bool {
	return s.ServiceName != RateLimitReportServiceName
}

// ProducedStorePaths returns the store paths that this spec's onReply chain will produce.
func (s ActionSpec) ProducedStorePaths() []string {
	switch s.ServiceName {
	case AuthServiceName:
		return []string{AuthStorePath}
	default:
		return nil
	}
}

// Build materializes this ActionSpec into a concrete Action by dispatching on ServiceName.
func (s ActionSpec) Build() Action {
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

// BuildActions materializes a slice of ActionSpecs into Actions.
// Body field references (responseBodyJSON/requestBodyJSON) across all specs are
// extracted into a single StoreAction per direction, prepended to the result.
func BuildActions(specs []ActionSpec) []Action {
	type refEntry struct {
		ref     bodyRef
		sources []string
	}

	// Collect all body refs across all specs, grouped by direction.
	// Key: direction ("response"/"request"), value: fieldName -> refEntry
	byDirection := make(map[string]map[string]refEntry)

	for _, spec := range specs {
		for _, cd := range spec.ConditionalData {
			for _, item := range cd.Data {
				if expr, ok := item.Value.(*Expression); ok {
					for _, ref := range extractBodyRefs(expr.ExpressionItem.Value) {
						if byDirection[ref.Direction] == nil {
							byDirection[ref.Direction] = make(map[string]refEntry)
						}
						entry := byDirection[ref.Direction][ref.FieldName]
						entry.ref = ref
						entry.sources = appendUnique(entry.sources, spec.Sources...)
						byDirection[ref.Direction][ref.FieldName] = entry
					}
				}
			}
		}
		for _, b := range spec.Bindings {
			for _, ref := range extractBodyRefs(b.Expression) {
				if byDirection[ref.Direction] == nil {
					byDirection[ref.Direction] = make(map[string]refEntry)
				}
				entry := byDirection[ref.Direction][ref.FieldName]
				entry.ref = ref
				entry.sources = appendUnique(entry.sources, spec.Sources...)
				byDirection[ref.Direction][ref.FieldName] = entry
			}
		}
	}

	if len(byDirection) == 0 {
		actions := make([]Action, len(specs))
		for i, spec := range specs {
			actions[i] = spec.Build()
		}
		return actions
	}

	// Build store actions and a replacement map
	replacements := make(map[string]string) // original call -> store path
	var storeActions []Action

	for _, direction := range []string{"request", "response"} {
		fields, ok := byDirection[direction]
		if !ok {
			continue
		}

		fieldNames := sortedKeys(fields)

		// Build map expression: {"field1": bodyJSON("/path1"), "field2": bodyJSON("/path2")}
		var mapEntries []string
		var allSources []string
		for _, fieldName := range fieldNames {
			entry := fields[fieldName]
			mapEntries = append(mapEntries, fmt.Sprintf(`"%s": %s`, fieldName, entry.ref.Original))
			replacements[entry.ref.Original] = bodyRefStorePath(direction, fieldName)
			allSources = appendUnique(allSources, entry.sources...)
		}

		var storePath string
		if direction == "request" {
			storePath = requestBodyStorePath
		} else {
			storePath = responseBodyStorePath
		}

		mapExpr := fmt.Sprintf("{%s}", strings.Join(mapEntries, ", "))
		storeActions = append(storeActions,
			NewStoreAction("true", storePath, mapExpr).
				WithGuard(false).
				WithSources(allSources),
		)
	}

	// Replace body refs in all specs and build
	modified := make([]ActionSpec, len(specs))
	for i, spec := range specs {
		modified[i] = spec.replaceBodyRefs(replacements)
	}

	actions := make([]Action, 0, len(storeActions)+len(modified))
	actions = append(actions, storeActions...)
	for _, spec := range modified {
		actions = append(actions, spec.Build())
	}
	return actions
}

func (s ActionSpec) replaceBodyRefs(replacements map[string]string) ActionSpec {
	newCD := make([]ConditionalData, len(s.ConditionalData))
	for i, cd := range s.ConditionalData {
		newData := make([]DataType, len(cd.Data))
		for j, item := range cd.Data {
			if expr, ok := item.Value.(*Expression); ok {
				newValue := expr.ExpressionItem.Value
				for original, storePath := range replacements {
					newValue = strings.ReplaceAll(newValue, original, storePath)
				}
				if newValue != expr.ExpressionItem.Value {
					newData[j] = DataType{Value: &Expression{
						ExpressionItem: ExpressionItem{
							Key:   expr.ExpressionItem.Key,
							Value: newValue,
						},
					}}
				} else {
					newData[j] = item
				}
			} else {
				newData[j] = item
			}
		}
		newCD[i] = ConditionalData{Predicates: cd.Predicates, Data: newData}
	}

	newBindings := make([]DataBinding, len(s.Bindings))
	for i, b := range s.Bindings {
		newExpr := b.Expression
		for original, storePath := range replacements {
			newExpr = strings.ReplaceAll(newExpr, original, storePath)
		}
		newBindings[i] = DataBinding{Domain: b.Domain, Field: b.Field, Expression: newExpr}
	}

	return ActionSpec{
		ServiceName:     s.ServiceName,
		Scope:           s.Scope,
		Predicates:      s.Predicates,
		ConditionalData: newCD,
		Sources:         s.Sources,
		Bindings:        newBindings,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func appendUnique(dest []string, items ...string) []string {
	seen := make(map[string]bool, len(dest))
	for _, s := range dest {
		seen[s] = true
	}
	for _, item := range items {
		if !seen[item] {
			dest = append(dest, item)
			seen[item] = true
		}
	}
	return dest
}

func (s ActionSpec) buildAuth() *GrpcAction {
	request := CheckRequestCEL{
		Scope:           s.Scope,
		MetadataContext: buildMetadataContext(s.Bindings),
	}
	predicate := buildActionPredicate(s.Predicates)

	return NewGrpcAction(predicate, authResponseVar, s.ServiceName, request.ToCEL(), "auth").
		WithSources(s.Sources).
		WithOnReply(buildAuthOnReply(authResponseVar)...)
}

func (s ActionSpec) buildRateLimit(responseVar string, isGuard bool, label string) *GrpcAction {
	request := buildRateLimitRequest(s.Scope, s.ConditionalData, s.Bindings)
	predicate := buildRateLimitPredicate(s.Predicates, s.ConditionalData)

	var onReply []Action
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

// DomainAndFieldName splits a binding key on the last "." into (domain, field).
// "auth.identity.user" → ("auth.identity", "user")
// "simple" → ("", "simple")
func DomainAndFieldName(name string) (string, string) {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}

func isResponsePhaseExpression(expr string) bool {
	return strings.Contains(expr, "responseBodyJSON(") ||
		strings.Contains(expr, "requestBodyJSON(") ||
		strings.Contains(expr, responseBodyStorePath) ||
		strings.Contains(expr, requestBodyStorePath)
}

// AttachBindings walks specs in pipeline order and attaches only bindings whose
// dependencies are satisfied at each position. Two checks are applied:
//   - Store-path availability: specs declare produced store paths via
//     ProducedStorePaths(); bindings referencing a path not yet produced are excluded.
//   - Response-phase access: guard specs (request phase) cannot evaluate
//     response-body expressions, so those bindings are excluded.
func AttachBindings(specs []ActionSpec, bindings []DataBinding) {
	if len(bindings) == 0 {
		return
	}

	pendingPaths := make(map[string]bool)
	for _, spec := range specs {
		for _, path := range spec.ProducedStorePaths() {
			pendingPaths[path] = true
		}
	}

	for i := range specs {
		for _, path := range specs[i].ProducedStorePaths() {
			delete(pendingPaths, path)
		}

		pending := make([]string, 0, len(pendingPaths))
		for p := range pendingPaths {
			pending = append(pending, p)
		}

		specs[i].Bindings = append(specs[i].Bindings,
			availableBindings(bindings, pending, specs[i].IsGuard())...)
	}
}

func availableBindings(bindings []DataBinding, pendingPaths []string, guard bool) []DataBinding {
	if len(pendingPaths) == 0 && !guard {
		return bindings
	}
	var filtered []DataBinding
	for _, b := range bindings {
		if referencesPendingPath(b.Expression, pendingPaths) {
			continue
		}
		if guard && isResponsePhaseExpression(b.Expression) {
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered
}

func referencesPendingPath(expr string, pendingPaths []string) bool {
	for _, path := range pendingPaths {
		if strings.Contains(expr, path+".") {
			return true
		}
	}
	return false
}

// bodyJSONPattern matches responseBodyJSON("...") and requestBodyJSON("...") with either quote style.
var bodyJSONPattern = regexp.MustCompile(`(response|request)BodyJSON\(["']([^"']+)["']\)`)

const (
	responseBodyStorePath = "kuadrant.internal.response.body"
	requestBodyStorePath  = "kuadrant.internal.request.body"
)

type bodyRef struct {
	Original  string // the full matched call, e.g. responseBodyJSON("/usage/total_tokens")
	Direction string // "response" or "request"
	FieldName string // derived map key, e.g. "total_tokens"
	Pointer   string // the JSON pointer, e.g. "/usage/total_tokens"
}

func bodyRefFieldName(jsonPointer string) string {
	segments := strings.Split(strings.TrimPrefix(jsonPointer, "/"), "/")
	return segments[len(segments)-1]
}

func bodyRefStorePath(direction, fieldName string) string {
	if direction == "request" {
		return requestBodyStorePath + "." + fieldName
	}
	return responseBodyStorePath + "." + fieldName
}

func extractBodyRefs(expr string) []bodyRef {
	matches := bodyJSONPattern.FindAllStringSubmatch(expr, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var refs []bodyRef
	for _, m := range matches {
		original := m[0]
		if seen[original] {
			continue
		}
		seen[original] = true
		refs = append(refs, bodyRef{
			Original:  original,
			Direction: m[1],
			FieldName: bodyRefFieldName(m[2]),
			Pointer:   m[2],
		})
	}
	return refs
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

func buildAuthOnReply(name string) []Action {
	return []Action{
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
			AuthStorePath,
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

func buildRateLimitOnReply(name string) []Action {
	return []Action{
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
		NewStoreAction("true", RateLimitCompleteSignal, "true"),
	}
}

func buildReportOnReply(name string) []Action {
	return []Action{
		NewFailAction(
			fmt.Sprintf("!has(%s.overall_code)", name),
			"Rate limit report failed: invalid gRPC response",
		).WithTerminal(false).WithGuard(false),
	}
}
