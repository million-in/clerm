package clermwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

type Value struct {
	Name string          `json:"name"`
	Type schema.ArgType  `json:"type"`
	Raw  json.RawMessage `json:"raw"`
}

var compactJSONBufferPool = sync.Pool{
	New: func() any {
		return &bytes.Buffer{}
	},
}

func BuildValues(params []schema.Parameter, payloadJSON []byte, kind string) ([]Value, error) {
	trimmed := bytes.TrimSpace(payloadJSON)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, fmt.Sprintf("parse %s JSON", kind))
	}
	paramNames := parameterNameSet(params)
	unknown := make([]string, 0)
	for key := range payload {
		if _, ok := paramNames[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, platform.New(platform.CodeValidation, fmt.Sprintf("unknown %s values: %s", kind, strings.Join(unknown, ", ")))
	}

	values := make([]Value, 0, len(params))
	for _, param := range params {
		raw, exists := payload[param.Name]
		if !exists {
			return nil, platform.New(platform.CodeValidation, fmt.Sprintf("missing required %s %s", kind, param.Name))
		}
		compact := compactJSONBufferPool.Get().(*bytes.Buffer)
		compact.Reset()
		if err := json.Compact(compact, raw); err != nil {
			compactJSONBufferPool.Put(compact)
			return nil, platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("invalid JSON for %s", param.Name))
		}
		normalized := json.RawMessage(append([]byte(nil), compact.Bytes()...))
		compactJSONBufferPool.Put(compact)
		if err := ValidateValue(param.Type, normalized); err != nil {
			return nil, platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("invalid value for %s", param.Name))
		}
		values = append(values, Value{Name: param.Name, Type: param.Type, Raw: normalized})
	}
	return values, nil
}

func BuildValuesFromMap(params []schema.Parameter, payload map[string]any, kind string) ([]Value, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	paramNames := parameterNameSet(params)
	unknown := make([]string, 0)
	for key := range payload {
		if _, ok := paramNames[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, platform.New(platform.CodeValidation, fmt.Sprintf("unknown %s values: %s", kind, strings.Join(unknown, ", ")))
	}
	values := make([]Value, 0, len(params))
	for _, param := range params {
		value, exists := payload[param.Name]
		if !exists {
			return nil, platform.New(platform.CodeValidation, fmt.Sprintf("missing required %s %s", kind, param.Name))
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("encode value for %s", param.Name))
		}
		if err := ValidateValue(param.Type, raw); err != nil {
			return nil, platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("invalid value for %s", param.Name))
		}
		values = append(values, Value{Name: param.Name, Type: param.Type, Raw: raw})
	}
	return values, nil
}

func ValidateValues(values []Value, params []schema.Parameter, kind string) error {
	if len(values) != len(params) {
		return platform.New(platform.CodeValidation, fmt.Sprintf("%s count does not match schema definition", kind))
	}
	for i, value := range values {
		expected := params[i]
		if value.Name != expected.Name {
			return platform.New(platform.CodeValidation, fmt.Sprintf("%s order mismatch for %s", kind, expected.Name))
		}
		if value.Type != expected.Type {
			return platform.New(platform.CodeValidation, fmt.Sprintf("%s type mismatch for %s", kind, value.Name))
		}
		if err := ValidateValue(value.Type, value.Raw); err != nil {
			return platform.Wrap(platform.CodeValidation, err, fmt.Sprintf("invalid %s %s", kind, value.Name))
		}
	}
	return nil
}

func ValuesAsMap(values []Value) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for _, value := range values {
		decoded, err := DecodeValue(value.Type, value.Raw)
		if err != nil {
			return nil, err
		}
		out[value.Name] = decoded
	}
	return out, nil
}

func ValidateValue(argType schema.ArgType, raw json.RawMessage) error {
	raw = trimJSONSpace(raw)
	if len(raw) == 0 {
		return platform.New(platform.CodeValidation, "value is required")
	}
	switch argType {
	case schema.ArgString:
		_, err := parseJSONString(raw)
		return err
	case schema.ArgDecimal:
		_, err := strconv.ParseFloat(bytesToString(raw), 64)
		if err != nil {
			return platform.Wrap(platform.CodeValidation, err, "decimal values must be valid JSON numbers")
		}
		return nil
	case schema.ArgUUID:
		value, err := parseJSONString(raw)
		if err != nil {
			return err
		}
		if !looksLikeUUID(value) {
			return platform.New(platform.CodeValidation, "UUID values must use canonical xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx format")
		}
		return nil
	case schema.ArgArray:
		if !hasJSONArrayEnvelope(raw) {
			return platform.New(platform.CodeValidation, "ARRAY values must use JSON array framing")
		}
		return nil
	case schema.ArgTimestamp:
		value, err := parseJSONString(raw)
		if err != nil {
			return err
		}
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return platform.New(platform.CodeValidation, "timestamps must be RFC3339 strings")
		}
		return nil
	case schema.ArgInt:
		if _, err := strconv.ParseInt(bytesToString(raw), 10, 64); err != nil {
			return platform.Wrap(platform.CodeValidation, err, "INT values must be valid JSON integers")
		}
		return nil
	case schema.ArgBool:
		if bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false")) {
			return nil
		}
		return platform.New(platform.CodeValidation, "BOOL values must be true or false")
	default:
		return platform.New(platform.CodeValidation, "unknown argument type")
	}
}

func DecodeValue(argType schema.ArgType, raw json.RawMessage) (any, error) {
	raw = trimJSONSpace(raw)
	switch argType {
	case schema.ArgString, schema.ArgUUID, schema.ArgTimestamp:
		return parseJSONString(raw)
	case schema.ArgDecimal:
		return strconv.ParseFloat(bytesToString(raw), 64)
	case schema.ArgArray:
		if err := ValidateValue(argType, raw); err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	case schema.ArgInt:
		return strconv.ParseInt(bytesToString(raw), 10, 64)
	case schema.ArgBool:
		switch bytesToString(raw) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, platform.New(platform.CodeValidation, "BOOL values must be true or false")
		}
	default:
		return nil, platform.New(platform.CodeValidation, "unknown argument type")
	}
}

func ValuesFromRequestArguments(args []struct {
	Name string
	Type schema.ArgType
	Raw  json.RawMessage
}) []Value {
	values := make([]Value, len(args))
	for i, arg := range args {
		values[i] = Value{Name: arg.Name, Type: arg.Type, Raw: arg.Raw}
	}
	return values
}

func parameterNameSet(params []schema.Parameter) map[string]struct{} {
	names := make(map[string]struct{}, len(params))
	for _, param := range params {
		names[param.Name] = struct{}{}
	}
	return names
}

func hasJSONArrayEnvelope(raw []byte) bool {
	return len(raw) >= 2 && raw[0] == '[' && raw[len(raw)-1] == ']'
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHex(r) {
				return false
			}
		}
	}
	return true
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func trimJSONSpace(raw []byte) []byte {
	start := 0
	end := len(raw)
	for start < end {
		switch raw[start] {
		case ' ', '\n', '\r', '\t':
			start++
		default:
			goto trimRight
		}
	}
trimRight:
	for end > start {
		switch raw[end-1] {
		case ' ', '\n', '\r', '\t':
			end--
		default:
			return raw[start:end]
		}
	}
	return raw[start:end]
}

func parseJSONString(raw []byte) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", platform.New(platform.CodeValidation, "STRING values must be valid JSON strings")
	}
	value := raw[1 : len(raw)-1]
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' || value[i] == '"' {
			decoded, err := strconv.Unquote(bytesToString(raw))
			if err != nil {
				return "", platform.Wrap(platform.CodeValidation, err, "STRING values must be valid JSON strings")
			}
			return decoded, nil
		}
	}
	return bytesToString(value), nil
}

func bytesToString(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(value), len(value))
}
