package schema

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/million-in/clerm/internal/platform"
)

func Parse(r io.Reader) (*Document, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	doc := &Document{}
	state := parserState{}
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if strings.ContainsRune(raw, '\t') {
			return nil, parseError(lineNo, "tabs are not allowed; use two-space indentation")
		}
		raw = stripComments(raw)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := leadingSpaces(raw)
		if indent%2 != 0 {
			return nil, parseError(lineNo, "indentation must be a multiple of 2 spaces")
		}
		text := strings.TrimSpace(raw)
		if strings.HasPrefix(text, "@routes.") {
			return nil, parseError(lineNo, "@routes declarations are not allowed in public .clermfile schemas")
		}
		switch indent {
		case 0:
			if err := beginTopLevel(doc, &state, text, lineNo); err != nil {
				return nil, err
			}
		case 2:
			if err := parseLevelOne(doc, &state, text, lineNo); err != nil {
				return nil, err
			}
		case 4:
			if err := parseLevelTwo(doc, &state, text, lineNo); err != nil {
				return nil, err
			}
		default:
			return nil, parseError(lineNo, "indentation depth is invalid for this grammar")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read schema file")
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

type parserState struct {
	section       string
	currentMethod *Method
	nestedSection string
}

func beginTopLevel(doc *Document, state *parserState, text string, lineNo int) error {
	state.nestedSection = ""
	switch {
	case strings.HasPrefix(text, "schema "):
		if doc.Name != "" {
			return parseError(lineNo, "schema declaration can only appear once")
		}
		doc.Name = strings.TrimSpace(strings.TrimPrefix(text, "schema "))
		if doc.Name == "" {
			return parseError(lineNo, "schema declaration name is required")
		}
		state.section = "schema"
		state.currentMethod = nil
		return nil
	case strings.HasPrefix(text, "method "):
		if doc.Name == "" {
			return parseError(lineNo, "schema declaration must appear before methods")
		}
		ref, err := ParseServiceRef(strings.TrimSpace(strings.TrimPrefix(text, "method ")))
		if err != nil {
			return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid method declaration"))
		}
		doc.Methods = append(doc.Methods, Method{Reference: ref, InputCount: -1, OutputCount: -1})
		state.section = "method"
		state.currentMethod = &doc.Methods[len(doc.Methods)-1]
		return nil
	case strings.HasPrefix(text, "relations "):
		if doc.Name == "" {
			return parseError(lineNo, "schema declaration must appear before relations")
		}
		if doc.RelationsName != "" {
			return parseError(lineNo, "relations declaration can only appear once")
		}
		doc.RelationsName = strings.TrimSpace(strings.TrimPrefix(text, "relations "))
		if doc.RelationsName == "" {
			return parseError(lineNo, "relations declaration name is required")
		}
		state.section = "relations"
		state.currentMethod = nil
		return nil
	default:
		return parseError(lineNo, "unknown top-level declaration")
	}
}

func parseLevelOne(doc *Document, state *parserState, text string, lineNo int) error {
	switch state.section {
	case "schema":
		key, value, err := parseAssignment(text)
		if err != nil {
			return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid schema entry"))
		}
		switch key {
		case "@metadata":
			if strings.TrimSpace(value) != "" {
				return parseError(lineNo, "@metadata must not have an inline value")
			}
			state.nestedSection = "metadata"
			return nil
		case "service":
			service, err := ParseServiceRef(value)
			if err != nil {
				return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid service declaration"))
			}
			doc.Services = append(doc.Services, service)
			state.nestedSection = ""
			return nil
		case "@route", "@routes", "route":
			if doc.Route != "" {
				return parseError(lineNo, "schema can only declare one route")
			}
			doc.Route = strings.TrimSpace(value)
			if doc.Route == "" {
				return parseError(lineNo, "route value is required")
			}
			state.nestedSection = ""
			return nil
		default:
			return parseError(lineNo, "schema section only accepts @metadata, service, and route declarations")
		}
	case "method":
		if state.currentMethod == nil {
			return parseError(lineNo, "method state is not initialized")
		}
		key, value, err := parseAssignment(text)
		if err != nil {
			return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid method entry"))
		}
		switch key {
		case "@exec":
			state.currentMethod.Execution, err = ParseExecutionMode(value)
			if err != nil {
				return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid @exec value"))
			}
			state.nestedSection = ""
			return nil
		case "@args_input":
			count, err := parseOptionalCount(value)
			if err != nil {
				return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid @args_input value"))
			}
			state.currentMethod.InputCount = count
			state.nestedSection = "input"
			return nil
		case "@args_output":
			count, err := parseOptionalCount(value)
			if err != nil {
				return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid @args_output value"))
			}
			state.currentMethod.OutputCount = count
			state.nestedSection = "output"
			return nil
		default:
			return parseError(lineNo, "unknown method declaration keyword")
		}
	case "relations":
		key, value, err := parseAssignment(text)
		if err != nil {
			return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid relation entry"))
		}
		doc.Relations = append(doc.Relations, RelationRule{Name: strings.TrimSpace(key), Condition: strings.TrimSpace(value)})
		return nil
	default:
		return parseError(lineNo, "unexpected indented declaration")
	}
}

func parseLevelTwo(doc *Document, state *parserState, text string, lineNo int) error {
	if state.section == "schema" && state.nestedSection == "metadata" {
		return parseMetadataField(doc, text, lineNo)
	}
	if state.section != "method" || state.currentMethod == nil || state.nestedSection == "" {
		return parseError(lineNo, "nested declarations are only valid under @metadata, @args_input, or @args_output")
	}
	key, value, err := parseAssignment(text)
	if err != nil {
		return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid nested declaration"))
	}
	switch key {
	case "decl_args":
		params, err := parseParameters(value)
		if err != nil {
			return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid decl_args value"))
		}
		if state.nestedSection == "input" {
			state.currentMethod.InputArgs = params
		} else {
			state.currentMethod.OutputArgs = params
		}
		return nil
	case "decl_format":
		if state.nestedSection != "output" {
			return parseError(lineNo, "decl_format is only allowed under @args_output")
		}
		state.currentMethod.OutputFormat, err = ParsePayloadFormat(value)
		if err != nil {
			return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid decl_format value"))
		}
		return nil
	default:
		return parseError(lineNo, "unknown nested declaration keyword")
	}
}

func parseMetadataField(doc *Document, text string, lineNo int) error {
	key, value, err := parseAssignment(text)
	if err != nil {
		return platform.Wrap(platform.CodeParse, err, lineMessage(lineNo, "invalid metadata entry"))
	}
	value = strings.TrimSpace(value)
	switch key {
	case "description":
		doc.Metadata.Description = value
		return nil
	case "display_name":
		doc.Metadata.DisplayName = value
		return nil
	case "category":
		doc.Metadata.Category = value
		return nil
	case "tags":
		doc.Metadata.Tags = parseCSV(value)
		return nil
	default:
		return parseError(lineNo, "unknown metadata field")
	}
}

func parseAssignment(text string) (string, string, error) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return "", "", platform.New(platform.CodeParse, "expected key: value")
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", platform.New(platform.CodeParse, "assignment key is required")
	}
	return key, value, nil
}

func parseOptionalCount(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return -1, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, platform.New(platform.CodeParse, "count must be a non-negative integer")
	}
	return count, nil
}

func parseParameters(raw string) ([]Parameter, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, platform.New(platform.CodeParse, "decl_args value is required")
	}
	parts := strings.Split(value, ",")
	params := make([]Parameter, 0, len(parts))
	for _, part := range parts {
		pair := strings.TrimSpace(part)
		segments := strings.Split(pair, ".")
		if len(segments) != 2 {
			return nil, platform.New(platform.CodeParse, fmt.Sprintf("invalid parameter declaration %q", pair))
		}
		name := strings.TrimSpace(segments[0])
		argType, err := ParseArgType(segments[1])
		if err != nil {
			return nil, err
		}
		params = append(params, Parameter{Name: name, Type: argType})
	}
	return params, nil
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func stripComments(raw string) string {
	if idx := strings.IndexRune(raw, '#'); idx >= 0 {
		return raw[:idx]
	}
	return raw
}

func leadingSpaces(raw string) int {
	count := 0
	for _, r := range raw {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

func parseError(lineNo int, message string) error {
	return platform.New(platform.CodeParse, lineMessage(lineNo, message))
}

func lineMessage(lineNo int, message string) string {
	return fmt.Sprintf("line %d: %s", lineNo, message)
}
