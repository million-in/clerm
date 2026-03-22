package jsonwire

import (
	"encoding/json"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

type configJSON struct {
	Name          string         `json:"name"`
	RelationsName string         `json:"relations_name"`
	Route         string         `json:"route,omitempty"`
	Services      []string       `json:"services"`
	Methods       []methodJSON   `json:"methods"`
	Relations     []relationJSON `json:"relations"`
}

type methodJSON struct {
	Reference    string          `json:"reference"`
	Execution    string          `json:"execution"`
	InputCount   int             `json:"input_count"`
	InputArgs    []parameterJSON `json:"input_args"`
	OutputCount  int             `json:"output_count"`
	OutputArgs   []parameterJSON `json:"output_args"`
	OutputFormat string          `json:"output_format"`
}

type parameterJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type relationJSON struct {
	Name      string `json:"name"`
	Condition string `json:"condition"`
}

type requestJSON struct {
	Method     string         `json:"method"`
	Arguments  []argumentJSON `json:"arguments"`
	Capability string         `json:"capability,omitempty"`
}

type argumentJSON struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func MarshalConfig(doc *schema.Document, includeRoute bool) ([]byte, error) {
	wire := configJSON{
		Name:          doc.Name,
		RelationsName: doc.RelationsName,
		Services:      make([]string, 0, len(doc.Services)),
		Methods:       make([]methodJSON, 0, len(doc.Methods)),
		Relations:     make([]relationJSON, 0, len(doc.Relations)),
	}
	if includeRoute {
		wire.Route = doc.Route
	}
	for _, service := range doc.Services {
		wire.Services = append(wire.Services, service.Raw)
	}
	for _, method := range doc.Methods {
		wire.Methods = append(wire.Methods, methodJSON{
			Reference:    method.Reference.Raw,
			Execution:    method.Execution.String(),
			InputCount:   method.InputCount,
			InputArgs:    marshalParameters(method.InputArgs),
			OutputCount:  method.OutputCount,
			OutputArgs:   marshalParameters(method.OutputArgs),
			OutputFormat: method.OutputFormat.String(),
		})
	}
	for _, relation := range doc.Relations {
		wire.Relations = append(wire.Relations, relationJSON{Name: relation.Name, Condition: relation.Condition})
	}
	return json.Marshal(wire)
}

func UnmarshalConfig(data []byte) (*schema.Document, error) {
	var wire configJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "parse config JSON")
	}
	doc := &schema.Document{
		Name:          wire.Name,
		RelationsName: wire.RelationsName,
		Route:         wire.Route,
	}
	for _, raw := range wire.Services {
		service, err := schema.ParseServiceRef(raw)
		if err != nil {
			return nil, err
		}
		doc.Services = append(doc.Services, service)
	}
	for _, methodWire := range wire.Methods {
		ref, err := schema.ParseServiceRef(methodWire.Reference)
		if err != nil {
			return nil, err
		}
		exec, err := schema.ParseExecutionMode(methodWire.Execution)
		if err != nil {
			return nil, err
		}
		format, err := schema.ParsePayloadFormat(methodWire.OutputFormat)
		if err != nil {
			return nil, err
		}
		doc.Methods = append(doc.Methods, schema.Method{
			Reference:    ref,
			Execution:    exec,
			InputCount:   methodWire.InputCount,
			InputArgs:    nil,
			OutputCount:  methodWire.OutputCount,
			OutputArgs:   nil,
			OutputFormat: format,
		})
		doc.Methods[len(doc.Methods)-1].InputArgs, err = unmarshalParameters(methodWire.InputArgs)
		if err != nil {
			return nil, err
		}
		doc.Methods[len(doc.Methods)-1].OutputArgs, err = unmarshalParameters(methodWire.OutputArgs)
		if err != nil {
			return nil, err
		}
	}
	for _, relation := range wire.Relations {
		doc.Relations = append(doc.Relations, schema.RelationRule{Name: relation.Name, Condition: relation.Condition})
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

func MarshalRequest(request *clermreq.Request) ([]byte, error) {
	wire := requestJSON{
		Method:    request.Method,
		Arguments: make([]argumentJSON, 0, len(request.Arguments)),
	}
	if len(request.CapabilityRaw) > 0 {
		token, err := capability.Decode(request.CapabilityRaw)
		if err != nil {
			return nil, err
		}
		wire.Capability, err = capability.EncodeText(token)
		if err != nil {
			return nil, err
		}
	}
	for _, arg := range request.Arguments {
		wire.Arguments = append(wire.Arguments, argumentJSON{
			Name:  arg.Name,
			Type:  arg.Type.String(),
			Value: append(json.RawMessage(nil), arg.Raw...),
		})
	}
	return json.Marshal(wire)
}

func UnmarshalRequest(data []byte) (*clermreq.Request, error) {
	var wire requestJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "parse request JSON")
	}
	request := &clermreq.Request{Method: wire.Method}
	for _, arg := range wire.Arguments {
		argType, err := schema.ParseArgType(arg.Type)
		if err != nil {
			return nil, err
		}
		request.Arguments = append(request.Arguments, clermreq.Argument{
			Name: arg.Name,
			Type: argType,
			Raw:  append(json.RawMessage(nil), arg.Value...),
		})
	}
	if wire.Capability != "" {
		token, err := capability.DecodeText(wire.Capability)
		if err != nil {
			return nil, err
		}
		request.CapabilityRaw, err = capability.Encode(token)
		if err != nil {
			return nil, err
		}
	}
	return request, nil
}

func marshalParameters(params []schema.Parameter) []parameterJSON {
	out := make([]parameterJSON, 0, len(params))
	for _, param := range params {
		out = append(out, parameterJSON{Name: param.Name, Type: param.Type.String()})
	}
	return out
}

func unmarshalParameters(params []parameterJSON) ([]schema.Parameter, error) {
	out := make([]schema.Parameter, 0, len(params))
	for _, param := range params {
		argType, err := schema.ParseArgType(param.Type)
		if err != nil {
			return nil, err
		}
		out = append(out, schema.Parameter{Name: param.Name, Type: argType})
	}
	return out, nil
}
