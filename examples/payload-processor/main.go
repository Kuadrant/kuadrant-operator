package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	xds "github.com/cncf/xds/go/xds/type/v3"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
	"github.com/envoyproxy/envoy/contrib/golang/filters/http/source/go/pkg/http"
)

const (
	// Export types
	ExportTypeDynamicMetadata = "dynamic_metadata"
	ExportTypeFilterState     = "filter_state"

	// Defaults
	defaultMaxBufferSize     = 512
	defaultPattern           = `"model"\s*:\s*"([^"]+)"`
	defaultContentType       = "application/json"
	defaultExportAs          = ExportTypeDynamicMetadata
	defaultMetadataNamespace = "envoy.filters.http.golang.payload_processor"
	defaultObjectKey         = "extracted_value"
	defaultObjectValue       = "self.extractedValue"
)

const Name = "payload_processor"

func init() {
	http.RegisterHttpFilterFactoryAndConfigParser(Name, filterFactory, &parser{})
}

// parser implements the config parser
type parser struct{}

func (p *parser) Parse(configAny *anypb.Any, callbacks api.ConfigCallbackHandler) (any, error) {
	// If no config provided, use defaults
	if configAny == nil {
		return newDefaultConfig()
	}

	// Unmarshal the protobuf Any into a TypedStruct
	configStruct := &xds.TypedStruct{}
	if err := configAny.UnmarshalTo(configStruct); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	cfg, err := newDefaultConfig()
	if err != nil {
		return nil, err
	}

	// Parse fields from the struct
	if configStruct.Value != nil && configStruct.Value.Fields != nil {
		fields := configStruct.Value.Fields

		// Parse pattern
		if pattern, ok := fields["pattern"]; ok {
			if str := pattern.GetStringValue(); str != "" {
				cfg.Pattern = str
			}
		}

		// Parse export_as
		if exportAs, ok := fields["export_as"]; ok {
			if str := exportAs.GetStringValue(); str != "" {
				if str != ExportTypeDynamicMetadata && str != ExportTypeFilterState {
					return nil, fmt.Errorf("invalid export_as value '%s', must be '%s' or '%s'",
						str, ExportTypeDynamicMetadata, ExportTypeFilterState)
				}
				cfg.ExportAs = str
			}
		}

		// Parse object_key
		if objectKey, ok := fields["object_key"]; ok {
			if str := objectKey.GetStringValue(); str != "" {
				cfg.ObjectKey = str
			}
		}

		// Parse object_value
		if objectValue, ok := fields["object_value"]; ok {
			if str := objectValue.GetStringValue(); str != "" {
				cfg.ObjectValue = str
				// Recompile CEL program with new expression
				if err := cfg.compileCELProgram(); err != nil {
					return nil, fmt.Errorf("failed to compile object_value CEL expression: %w", err)
				}
			}
		}

		// Parse metadata_namespace (only relevant for dynamic_metadata)
		if metadataNamespace, ok := fields["metadata_namespace"]; ok {
			if str := metadataNamespace.GetStringValue(); str != "" {
				cfg.MetadataNamespace = str
			}
		}

		// Parse buffer size
		if bufferSize, ok := fields["max_buffer_size"]; ok {
			if num := bufferSize.GetNumberValue(); num > 0 {
				cfg.MaxBufferSize = int(num)
			}
		}

		// Parse content types
		if contentTypes, ok := fields["content_types"]; ok {
			if list := contentTypes.GetListValue(); list != nil {
				cfg.ContentTypes = make([]string, 0, len(list.Values))
				for _, val := range list.Values {
					if str := val.GetStringValue(); str != "" {
						cfg.ContentTypes = append(cfg.ContentTypes, str)
					}
				}
			}
		}
	}

	return cfg, nil
}

func (p *parser) Merge(parent any, child any) any {
	// Child config takes precedence over parent
	if child != nil {
		return child
	}
	return parent
}

// config holds filter configuration
type config struct {
	// Pattern is the regex pattern to search for in the request body
	// The first capture group will be extracted as self.extractedValue in CEL
	Pattern string `json:"pattern"`

	// ExportAs determines where to export the data: "dynamic_metadata" or "filter_state"
	ExportAs string `json:"export_as"`

	// ObjectKey is the key name for the exported data
	// - For dynamic_metadata: the key within the metadata_namespace
	// - For filter_state: the full filter state key (e.g., "envoy.filters.http.golang.payload_processor.model")
	ObjectKey string `json:"object_key"`

	// ObjectValue is a CEL expression that evaluates to the value to export
	// Available variables: self.extractedValue (the matched regex group)
	// Examples: "self.extractedValue", '"{" + self.extractedValue + "}"'
	ObjectValue string `json:"object_value"`

	// MetadataNamespace is the namespace for dynamic_metadata export (optional, only used if export_as=dynamic_metadata)
	MetadataNamespace string `json:"metadata_namespace,omitempty"`

	// MaxBufferSize is the maximum size of the rolling buffer
	MaxBufferSize int `json:"max_buffer_size"`

	// ContentTypes is a list of content types to process
	ContentTypes []string `json:"content_types"`

	// Internal: compiled regex pattern
	compiledPattern *regexp.Regexp

	// Internal: compiled CEL program for object_value
	celEnv     *cel.Env
	celProgram cel.Program
}

func newDefaultConfig() (*config, error) {
	cfg := &config{
		Pattern:           defaultPattern,
		ExportAs:          defaultExportAs,
		ObjectKey:         defaultObjectKey,
		ObjectValue:       defaultObjectValue,
		MetadataNamespace: defaultMetadataNamespace,
		MaxBufferSize:     defaultMaxBufferSize,
		ContentTypes:      []string{defaultContentType},
	}

	// Compile the default CEL program
	if err := cfg.compileCELProgram(); err != nil {
		return nil, fmt.Errorf("failed to compile default CEL program: %w", err)
	}

	return cfg, nil
}

// compileCELProgram compiles the ObjectValue CEL expression
func (c *config) compileCELProgram() error {
	// Create CEL environment with self.extractedValue variable
	env, err := cel.NewEnv(
		cel.Variable("self", cel.MapType(cel.StringType, cel.StringType)),
	)
	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Compile the expression
	ast, issues := env.Compile(c.ObjectValue)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("CEL compilation error: %w", issues.Err())
	}

	// Create program
	program, err := env.Program(ast)
	if err != nil {
		return fmt.Errorf("failed to create CEL program: %w", err)
	}

	c.celEnv = env
	c.celProgram = program
	return nil
}

// evaluateObjectValue evaluates the ObjectValue CEL expression with the extracted value
func (c *config) evaluateObjectValue(extractedValue string) (string, error) {
	// Create evaluation context with self.extractedValue
	selfMap := map[string]interface{}{
		"extractedValue": extractedValue,
	}

	// Evaluate the expression
	out, _, err := c.celProgram.Eval(map[string]interface{}{
		"self": selfMap,
	})
	if err != nil {
		return "", fmt.Errorf("CEL evaluation error: %w", err)
	}

	// Convert result to string
	switch v := out.Value().(type) {
	case string:
		return v, nil
	case ref.Val:
		return fmt.Sprintf("%v", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// String returns a JSON representation of the config for logging
func (c *config) String() string {
	data, _ := json.Marshal(c)
	return string(data)
}

// filterFactory creates new filter instances
func filterFactory(c any, callbacks api.FilterCallbackHandler) api.StreamFilter {
	cfg, ok := c.(*config)
	if !ok {
		cfg, _ = newDefaultConfig()
	}

	return &filter{
		config:    cfg,
		callbacks: callbacks,
	}
}

// filter implements the Envoy HTTP filter interface
type filter struct {
	api.PassThroughStreamFilter // Embed to get default implementations

	config         *config
	callbacks      api.FilterCallbackHandler
	rollingBuffer  string
	patternFound   bool
	extractedValue string
}

// DecodeHeaders is called when request headers are received
func (f *filter) DecodeHeaders(headers api.RequestHeaderMap, endOfStream bool) api.StatusType {
	// Compile the regex pattern from config
	var err error
	f.config.compiledPattern, err = regexp.Compile(f.config.Pattern)
	if err != nil {
		f.callbacks.Log(api.Error, fmt.Sprintf("Failed to compile pattern '%s': %v", f.config.Pattern, err))
		return api.Continue
	}

	// Check if the content type matches any of the configured types
	contentType, _ := headers.Get("content-type")
	contentTypeLower := strings.ToLower(contentType)

	matchesContentType := false
	for _, acceptedType := range f.config.ContentTypes {
		if strings.Contains(contentTypeLower, strings.ToLower(acceptedType)) {
			matchesContentType = true
			break
		}
	}

	if !matchesContentType {
		// Content type doesn't match, skip processing
		f.callbacks.Log(api.Debug, fmt.Sprintf("Skipping request with content-type '%s' (accepted: %v)", contentType, f.config.ContentTypes))
		return api.Continue
	}

	// If there's no body (endOfStream=true), continue
	if endOfStream {
		f.callbacks.Log(api.Debug, "No request body")
		return api.Continue
	}

	f.callbacks.Log(api.Info, fmt.Sprintf("Request detected (content-type: %s), blocking filter chain until pattern extracted", contentType))

	// Block the filter chain and buffer the request body
	// Unfortunately, Envoy 1.38.0's Golang filter only supports StopAndBuffer (3) and Continue (2)
	// This means we must buffer the entire body - there's no streaming option in this version
	// Memory usage = max concurrent requests × average request body size
	return api.StopAndBuffer
}

// DecodeData is called for each chunk of request body data
func (f *filter) DecodeData(buffer api.BufferInstance, endOfStream bool) api.StatusType {
	// If we already found the pattern, just continue
	if f.patternFound {
		return api.Continue
	}

	// Get the current chunk
	chunk := buffer.String()

	// Append to rolling buffer
	f.rollingBuffer += chunk

	// Keep only the last configured buffer size to handle patterns split across chunks
	// This prevents unbounded memory growth while still catching patterns that span chunk boundaries
	if len(f.rollingBuffer) > f.config.MaxBufferSize {
		f.rollingBuffer = f.rollingBuffer[len(f.rollingBuffer)-f.config.MaxBufferSize:]
	}

	// Search for the pattern in the rolling buffer
	matches := f.config.compiledPattern.FindStringSubmatch(f.rollingBuffer)
	if len(matches) > 1 {
		// Found the pattern! Extract it (matches[1] is the first capture group)
		f.extractedValue = matches[1]
		f.patternFound = true

		f.callbacks.Log(api.Info, fmt.Sprintf("Pattern found in payload: %s", f.extractedValue))

		// Evaluate the object_value CEL expression
		objectValue, err := f.config.evaluateObjectValue(f.extractedValue)
		if err != nil {
			f.callbacks.Log(api.Error, fmt.Sprintf("Failed to evaluate object_value CEL expression: %v", err))
			return api.Continue
		}

		f.callbacks.Log(api.Debug, fmt.Sprintf("Evaluated object_value: %s", objectValue))

		// Export data based on export_as config
		switch f.config.ExportAs {
		case ExportTypeDynamicMetadata:
			// Export to dynamic metadata
			if f.config.MetadataNamespace == "" {
				f.callbacks.Log(api.Error, "metadata_namespace is required when export_as=dynamic_metadata")
				return api.Continue
			}
			f.callbacks.StreamInfo().DynamicMetadata().Set(
				f.config.MetadataNamespace,
				f.config.ObjectKey,
				objectValue,
			)
			f.callbacks.Log(api.Info, fmt.Sprintf("Exported to dynamic_metadata[%s][%s] = %s",
				f.config.MetadataNamespace, f.config.ObjectKey, objectValue))

		case ExportTypeFilterState:
			// Export to filter state
			// WASM reads filter_state via get_property(), which requires:
			// - StateType: Mutable allows other filters to read/modify
			// - LifeSpan: Request scope for visibility during request processing
			// - StreamSharing: None for simplest case
			f.callbacks.StreamInfo().FilterState().SetString(
				f.config.ObjectKey,
				objectValue,
				api.StateTypeMutable, // Changed to Mutable - allows wasm to read
				api.LifeSpanRequest,  // Request scope (not Connection)
				api.None,             // No sharing (simplest case)
			)
			f.callbacks.Log(api.Info, fmt.Sprintf("Exported to filter_state[%s] = %s (StateType=Mutable, LifeSpan=Request, Sharing=None)",
				f.config.ObjectKey, objectValue))

		default:
			f.callbacks.Log(api.Error, fmt.Sprintf("Invalid export_as value: %s", f.config.ExportAs))
		}

		// Resume the filter chain - downstream filters will now execute with metadata/filter_state available
		return api.Continue
	}

	// Pattern not found yet
	if endOfStream {
		// End of stream reached without finding the pattern
		f.callbacks.Log(api.Debug, "Pattern not found in request body")
		return api.Continue
	}

	// Continue processing next chunk
	// Since we used StopAndBuffer in DecodeHeaders, the body is already being buffered
	// We just process each chunk as it arrives
	return api.Continue
}

func main() {}
