package schema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/million-in/clerm/internal/platform"
)

type ExecutionMode uint8

const (
	ExecutionUnknown ExecutionMode = iota
	ExecutionSync
	ExecutionAsyncPool
)

func ParseExecutionMode(raw string) (ExecutionMode, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "sync":
		return ExecutionSync, nil
	case "async.pool":
		return ExecutionAsyncPool, nil
	default:
		return ExecutionUnknown, platform.New(platform.CodeValidation, "unknown execution mode")
	}
}

func (e ExecutionMode) String() string {
	switch e {
	case ExecutionSync:
		return "sync"
	case ExecutionAsyncPool:
		return "async.pool"
	default:
		return "unknown"
	}
}

func (e ExecutionMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

type ArgType uint8

const (
	ArgUnknown ArgType = iota
	ArgString
	ArgDecimal
	ArgUUID
	ArgArray
	ArgTimestamp
	ArgInt
	ArgBool
)

func ParseArgType(raw string) (ArgType, error) {
	switch strings.TrimSpace(strings.ToUpper(raw)) {
	case "STRING":
		return ArgString, nil
	case "DECIMAL":
		return ArgDecimal, nil
	case "UUID":
		return ArgUUID, nil
	case "ARRAY":
		return ArgArray, nil
	case "TIMESTAMP":
		return ArgTimestamp, nil
	case "INT":
		return ArgInt, nil
	case "BOOL", "BOOLEAN":
		return ArgBool, nil
	default:
		return ArgUnknown, platform.New(platform.CodeValidation, "unknown argument type")
	}
}

func (a ArgType) String() string {
	switch a {
	case ArgString:
		return "STRING"
	case ArgDecimal:
		return "DECIMAL"
	case ArgUUID:
		return "UUID"
	case ArgArray:
		return "ARRAY"
	case ArgTimestamp:
		return "TIMESTAMP"
	case ArgInt:
		return "INT"
	case ArgBool:
		return "BOOL"
	default:
		return "UNKNOWN"
	}
}

func (a ArgType) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

type PayloadFormat uint8

const (
	FormatUnknown PayloadFormat = iota
	FormatJSON
	FormatXML
	FormatYAML
)

func ParsePayloadFormat(raw string) (PayloadFormat, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "json":
		return FormatJSON, nil
	case "xml":
		return FormatXML, nil
	case "yaml":
		return FormatYAML, nil
	default:
		return FormatUnknown, platform.New(platform.CodeValidation, "unknown payload format")
	}
}

func (f PayloadFormat) String() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatXML:
		return "xml"
	case FormatYAML:
		return "yaml"
	default:
		return "unknown"
	}
}

func (f PayloadFormat) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.String())
}

type ServiceRef struct {
	Raw       string `json:"raw"`
	Relation  string `json:"relation"`
	Namespace string `json:"namespace"`
	Method    string `json:"method"`
	Version   string `json:"version"`
}

func ParseServiceRef(raw string) (ServiceRef, error) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "@") {
		return ServiceRef{}, platform.New(platform.CodeValidation, "service reference must start with @")
	}
	firstDot := strings.IndexByte(value, '.')
	lastDot := strings.LastIndexByte(value, '.')
	if firstDot <= 1 || lastDot <= firstDot {
		return ServiceRef{}, platform.New(platform.CodeValidation, "service reference must include relation, service, method, and version")
	}
	secondLastDot := strings.LastIndexByte(value[:lastDot], '.')
	if secondLastDot <= firstDot {
		return ServiceRef{}, platform.New(platform.CodeValidation, "service reference must include relation, service, method, and version")
	}

	relation := value[:firstDot]
	namespace := value[firstDot+1 : secondLastDot]
	method := value[secondLastDot+1 : lastDot]
	version := value[lastDot+1:]

	if relation == "@" || namespace == "" || method == "" || version == "" {
		return ServiceRef{}, platform.New(platform.CodeValidation, "service reference contains empty segments")
	}
	if strings.Contains(namespace, "..") || strings.HasPrefix(namespace, ".") || strings.HasSuffix(namespace, ".") {
		return ServiceRef{}, platform.New(platform.CodeValidation, "service reference contains empty segments")
	}
	return ServiceRef{
		Raw:       value,
		Relation:  relation,
		Namespace: namespace,
		Method:    method,
		Version:   version,
	}, nil
}

type Parameter struct {
	Name string  `json:"name"`
	Type ArgType `json:"type"`
}

type Method struct {
	Reference    ServiceRef    `json:"reference"`
	Execution    ExecutionMode `json:"execution"`
	InputCount   int           `json:"input_count"`
	InputArgs    []Parameter   `json:"input_args"`
	OutputCount  int           `json:"output_count"`
	OutputArgs   []Parameter   `json:"output_args"`
	OutputFormat PayloadFormat `json:"output_format"`
}

type RelationRule struct {
	Name      string `json:"name"`
	Condition string `json:"condition"`
}

type Metadata struct {
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Category    string   `json:"category,omitempty"`
}

type Document struct {
	Name          string         `json:"name"`
	RelationsName string         `json:"relations_name"`
	Metadata      Metadata       `json:"metadata,omitempty"`
	Route         string         `json:"-"`
	Services      []ServiceRef   `json:"services"`
	Methods       []Method       `json:"methods"`
	Relations     []RelationRule `json:"relations"`
}

func (d *Document) Validate() error {
	if d == nil {
		return platform.New(platform.CodeInvalidArgument, "document is required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return platform.New(platform.CodeValidation, "schema declaration name is required")
	}
	if len(d.Services) == 0 {
		return platform.New(platform.CodeValidation, "at least one service declaration is required")
	}
	if len(d.Methods) == 0 {
		return platform.New(platform.CodeValidation, "at least one method declaration is required")
	}
	if len(d.Relations) == 0 {
		return platform.New(platform.CodeValidation, "at least one relation rule is required")
	}
	if err := validateRoute(d.Route); err != nil {
		return err
	}
	if err := validateMetadata(d.Metadata); err != nil {
		return err
	}

	for i, service := range d.Services {
		if err := validateServiceRef(service); err != nil {
			return platform.Wrap(platform.CodeValidation, err, "invalid service declaration")
		}
		for j := 0; j < i; j++ {
			if d.Services[j].Raw == service.Raw {
				return platform.New(platform.CodeValidation, fmt.Sprintf("duplicate service declaration %s", service.Raw))
			}
		}
	}

	for i, method := range d.Methods {
		if err := validateServiceRef(method.Reference); err != nil {
			return platform.Wrap(platform.CodeValidation, err, "invalid method declaration")
		}
		if !containsService(d.Services, method.Reference.Raw) {
			return platform.New(platform.CodeValidation, "method must be declared in schema avail before it is defined")
		}
		for j := 0; j < i; j++ {
			if d.Methods[j].Reference.Raw == method.Reference.Raw {
				return platform.New(platform.CodeValidation, fmt.Sprintf("duplicate method declaration %s", method.Reference.Raw))
			}
		}
		if method.Execution == ExecutionUnknown {
			return platform.New(platform.CodeValidation, fmt.Sprintf("method %s must declare a valid @exec", method.Reference.Raw))
		}
		if method.InputCount >= 0 && method.InputCount != len(method.InputArgs) {
			return platform.New(platform.CodeValidation, fmt.Sprintf("@args_input count does not match decl_args for %s", method.Reference.Raw))
		}
		if method.OutputCount >= 0 && method.OutputCount != len(method.OutputArgs) {
			return platform.New(platform.CodeValidation, fmt.Sprintf("@args_output count does not match decl_args for %s", method.Reference.Raw))
		}
		if len(method.InputArgs) == 0 && method.InputCount > 0 {
			return platform.New(platform.CodeValidation, fmt.Sprintf("method %s must declare input decl_args", method.Reference.Raw))
		}
		if len(method.OutputArgs) == 0 {
			return platform.New(platform.CodeValidation, fmt.Sprintf("method %s must declare output decl_args", method.Reference.Raw))
		}
		if method.OutputFormat == FormatUnknown {
			return platform.New(platform.CodeValidation, fmt.Sprintf("method %s must declare a valid decl_format", method.Reference.Raw))
		}
		if err := validateParameters(method.InputArgs); err != nil {
			return platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("invalid input parameters for %s", method.Reference.Raw))
		}
		if err := validateParameters(method.OutputArgs); err != nil {
			return platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("invalid output parameters for %s", method.Reference.Raw))
		}
	}

	for i, relation := range d.Relations {
		name := strings.TrimSpace(relation.Name)
		condition := strings.TrimSpace(relation.Condition)
		if name == "" || condition == "" {
			return platform.New(platform.CodeValidation, "relation rules must include a name and condition")
		}
		for j := 0; j < i; j++ {
			if d.Relations[j].Name == name {
				return platform.New(platform.CodeValidation, fmt.Sprintf("duplicate relation rule %s", name))
			}
		}
	}

	var missing []string
	for _, service := range d.Services {
		if !containsRelation(d.Relations, service.Relation) && !containsString(missing, service.Relation) {
			missing = append(missing, service.Relation)
		}
	}
	for _, method := range d.Methods {
		relation := method.Reference.Relation
		if !containsRelation(d.Relations, relation) && !containsString(missing, relation) {
			missing = append(missing, relation)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return platform.New(platform.CodeValidation, fmt.Sprintf("every used relation type must be defined in relations: %s", strings.Join(missing, ", ")))
	}

	return nil
}

func validateRoute(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return platform.New(platform.CodeValidation, "schema must declare one @route")
	}
	switch {
	case strings.HasPrefix(value, "env."):
		name := strings.TrimPrefix(value, "env.")
		if !isEnvName(name) {
			return platform.New(platform.CodeValidation, "route env reference must use env.VAR_NAME format")
		}
		return nil
	case strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}"):
		name := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
		if !isEnvName(name) {
			return platform.New(platform.CodeValidation, "route env reference must use ${VAR_NAME} format")
		}
		return nil
	default:
		schemeEnd := strings.Index(value, "://")
		if schemeEnd <= 0 {
			return platform.New(platform.CodeValidation, "route must be an absolute URL or env reference")
		}
		hostStart := schemeEnd + 3
		if hostStart >= len(value) {
			return platform.New(platform.CodeValidation, "route must be an absolute URL or env reference")
		}
		hostEnd := len(value)
		for i := hostStart; i < len(value); i++ {
			switch value[i] {
			case '/', '?', '#':
				hostEnd = i
				i = len(value)
			}
		}
		if hostEnd <= hostStart {
			return platform.New(platform.CodeValidation, "route must be an absolute URL or env reference")
		}
		return nil
	}
}

func (d *Document) MethodByReference(raw string) (Method, bool) {
	for _, method := range d.Methods {
		if method.Reference.Raw == raw {
			return method, true
		}
	}
	return Method{}, false
}

func (d *Document) RelationCondition(name string) (string, bool) {
	if d == nil {
		return "", false
	}
	for _, relation := range d.Relations {
		if relation.Name == name {
			return relation.Condition, true
		}
	}
	return "", false
}

func (d *Document) PublicFingerprint() [32]byte {
	sum := sha256.New()
	writeHashString(sum, d.Name)
	writeHashString(sum, d.RelationsName)
	writeHashUint16(sum, uint16(len(d.Services)))
	for _, service := range d.Services {
		writeHashString(sum, service.Raw)
	}
	writeHashUint16(sum, uint16(len(d.Methods)))
	for _, method := range d.Methods {
		writeHashString(sum, method.Reference.Raw)
		sum.Write([]byte{byte(method.Execution)})
		writeHashInt64(sum, int64(method.InputCount))
		writeHashParameters(sum, method.InputArgs)
		writeHashInt64(sum, int64(method.OutputCount))
		writeHashParameters(sum, method.OutputArgs)
		sum.Write([]byte{byte(method.OutputFormat)})
	}
	writeHashUint16(sum, uint16(len(d.Relations)))
	for _, relation := range d.Relations {
		writeHashString(sum, relation.Name)
		writeHashString(sum, relation.Condition)
	}
	var out [32]byte
	copy(out[:], sum.Sum(nil))
	return out
}

func validateParameters(params []Parameter) error {
	for i, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			return platform.New(platform.CodeValidation, "parameter name is required")
		}
		if param.Type == ArgUnknown {
			return platform.New(platform.CodeValidation, fmt.Sprintf("parameter %s has unknown type", name))
		}
		for j := 0; j < i; j++ {
			if params[j].Name == name {
				return platform.New(platform.CodeValidation, fmt.Sprintf("duplicate parameter %s", name))
			}
		}
	}
	return nil
}

func validateMetadata(metadata Metadata) error {
	if strings.TrimSpace(metadata.Description) == "" && strings.TrimSpace(metadata.DisplayName) == "" && strings.TrimSpace(metadata.Category) == "" && len(metadata.Tags) == 0 {
		return nil
	}
	if strings.TrimSpace(metadata.Description) == "" && strings.TrimSpace(metadata.DisplayName) == "" {
		return platform.New(platform.CodeValidation, "metadata must include at least description or display_name")
	}
	seen := make(map[string]struct{}, len(metadata.Tags))
	for _, tag := range metadata.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return platform.New(platform.CodeValidation, "metadata tags cannot be empty")
		}
		if _, ok := seen[tag]; ok {
			return platform.New(platform.CodeValidation, fmt.Sprintf("duplicate metadata tag %s", tag))
		}
		seen[tag] = struct{}{}
	}
	return nil
}

func validateServiceRef(ref ServiceRef) error {
	if strings.TrimSpace(ref.Raw) == "" {
		return platform.New(platform.CodeValidation, "service reference raw value is required")
	}
	if !strings.HasPrefix(ref.Relation, "@") || len(ref.Relation) == 1 {
		return platform.New(platform.CodeValidation, "service reference relation is invalid")
	}
	if strings.TrimSpace(ref.Namespace) == "" || strings.Contains(ref.Namespace, "..") {
		return platform.New(platform.CodeValidation, "service reference namespace is invalid")
	}
	if strings.TrimSpace(ref.Method) == "" {
		return platform.New(platform.CodeValidation, "service reference method is invalid")
	}
	if strings.TrimSpace(ref.Version) == "" {
		return platform.New(platform.CodeValidation, "service reference version is invalid")
	}
	return nil
}

func containsService(services []ServiceRef, raw string) bool {
	for _, service := range services {
		if service.Raw == raw {
			return true
		}
	}
	return false
}

func containsRelation(relations []RelationRule, name string) bool {
	for _, relation := range relations {
		if relation.Name == name {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeHashParameters(sum interface{ Write([]byte) (int, error) }, params []Parameter) {
	writeHashUint16(sum, uint16(len(params)))
	for _, param := range params {
		writeHashString(sum, param.Name)
		sum.Write([]byte{byte(param.Type)})
	}
}

func writeHashString(sum interface{ Write([]byte) (int, error) }, value string) {
	writeHashUint16(sum, uint16(len(value)))
	sum.Write([]byte(value))
}

func writeHashUint16(sum interface{ Write([]byte) (int, error) }, value uint16) {
	sum.Write([]byte{byte(value >> 8), byte(value)})
}

func writeHashInt64(sum interface{ Write([]byte) (int, error) }, value int64) {
	u := uint64(value)
	sum.Write([]byte{
		byte(u >> 56), byte(u >> 48), byte(u >> 40), byte(u >> 32),
		byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u),
	})
}

func isEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		case c == '_':
		default:
			return false
		}
	}
	return value[0] >= 'A' && value[0] <= 'Z'
}
