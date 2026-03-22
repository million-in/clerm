package schema

import (
	"bufio"
	"errors"
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
			return nil, parseErrorAt(lineNo, raw, strings.IndexByte(raw, '\t')+1, 1, "tabs are not allowed; use two-space indentation")
		}
		clean := stripComments(raw)
		if strings.TrimSpace(clean) == "" {
			continue
		}
		indent := leadingSpaces(clean)
		if indent%2 != 0 {
			return nil, parseErrorAt(lineNo, clean, indent+1, 1, "indentation must be a multiple of 2 spaces")
		}
		text := strings.TrimSpace(clean)
		if strings.HasPrefix(text, "@routes.") {
			return nil, parseErrorAt(lineNo, text, 1, len("@routes"), "@routes declarations are not allowed in public .clermfile schemas")
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
			return nil, parseErrorAt(lineNo, clean, indent+1, 1, "indentation depth is invalid for this grammar")
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
			return parseErrorAt(lineNo, text, 1, len("schema"), "schema declaration can only appear once")
		}
		doc.Name = strings.TrimSpace(strings.TrimPrefix(text, "schema "))
		if doc.Name == "" {
			return parseErrorAt(lineNo, text, len("schema ")+1, 1, "schema declaration name is required")
		}
		state.section = "schema"
		state.currentMethod = nil
		return nil
	case strings.HasPrefix(text, "method "):
		if doc.Name == "" {
			return parseErrorAt(lineNo, text, 1, len("method"), "schema declaration must appear before methods")
		}
		ref, err := ParseServiceRef(strings.TrimSpace(strings.TrimPrefix(text, "method ")))
		if err != nil {
			return parseWrapAt(lineNo, text, len("method ")+1, len(strings.TrimSpace(strings.TrimPrefix(text, "method "))), "invalid method declaration", err)
		}
		doc.Methods = append(doc.Methods, Method{Reference: ref, InputCount: -1, OutputCount: -1})
		state.section = "method"
		state.currentMethod = &doc.Methods[len(doc.Methods)-1]
		return nil
	case strings.HasPrefix(text, "relations "):
		if doc.Name == "" {
			return parseErrorAt(lineNo, text, 1, len("relations"), "schema declaration must appear before relations")
		}
		if doc.RelationsName != "" {
			return parseErrorAt(lineNo, text, 1, len("relations"), "relations declaration can only appear once")
		}
		doc.RelationsName = strings.TrimSpace(strings.TrimPrefix(text, "relations "))
		if doc.RelationsName == "" {
			return parseErrorAt(lineNo, text, len("relations ")+1, 1, "relations declaration name is required")
		}
		state.section = "relations"
		state.currentMethod = nil
		return nil
	default:
		token := firstToken(text)
		return parseErrorAt(lineNo, text, 1, spanWidth(token), "unknown top-level declaration; expected schema, method, or relations")
	}
}

func parseLevelOne(doc *Document, state *parserState, text string, lineNo int) error {
	switch state.section {
	case "schema":
		assignment, err := parseAssignment(text)
		if err != nil {
			return parseWrapAt(lineNo, text, 1, spanWidth(firstToken(text)), "invalid schema entry", err)
		}
		switch assignment.Key {
		case "@metadata":
			if strings.TrimSpace(assignment.Value) != "" {
				return parseErrorAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "@metadata must not have an inline value")
			}
			state.nestedSection = "metadata"
			return nil
		case "service":
			service, err := ParseServiceRef(assignment.Value)
			if err != nil {
				return parseWrapAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "invalid service declaration", err)
			}
			doc.Services = append(doc.Services, service)
			state.nestedSection = ""
			return nil
		case "@route", "@routes", "route":
			if doc.Route != "" {
				return parseErrorAt(lineNo, text, assignment.KeyColumn, assignment.KeyWidth, "schema can only declare one route")
			}
			doc.Route = strings.TrimSpace(assignment.Value)
			if doc.Route == "" {
				return parseErrorAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "route value is required")
			}
			state.nestedSection = ""
			return nil
		default:
			return parseErrorAt(lineNo, text, assignment.KeyColumn, assignment.KeyWidth, "schema section only accepts @metadata, service, and route declarations")
		}
	case "method":
		if state.currentMethod == nil {
			return parseErrorAt(lineNo, text, 1, spanWidth(firstToken(text)), "method state is not initialized")
		}
		assignment, err := parseAssignment(text)
		if err != nil {
			return parseWrapAt(lineNo, text, 1, spanWidth(firstToken(text)), "invalid method entry", err)
		}
		switch assignment.Key {
		case "@exec":
			state.currentMethod.Execution, err = ParseExecutionMode(assignment.Value)
			if err != nil {
				return parseWrapAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "invalid @exec value", err)
			}
			state.nestedSection = ""
			return nil
		case "@args_input":
			count, err := parseOptionalCount(assignment.Value)
			if err != nil {
				return parseWrapAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "invalid @args_input value", err)
			}
			state.currentMethod.InputCount = count
			state.nestedSection = "input"
			return nil
		case "@args_output":
			count, err := parseOptionalCount(assignment.Value)
			if err != nil {
				return parseWrapAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "invalid @args_output value", err)
			}
			state.currentMethod.OutputCount = count
			state.nestedSection = "output"
			return nil
		default:
			return parseErrorAt(lineNo, text, assignment.KeyColumn, assignment.KeyWidth, "unknown method declaration keyword; expected @exec, @args_input, or @args_output")
		}
	case "relations":
		assignment, err := parseAssignment(text)
		if err != nil {
			return parseWrapAt(lineNo, text, 1, spanWidth(firstToken(text)), "invalid relation entry", err)
		}
		doc.Relations = append(doc.Relations, RelationRule{Name: strings.TrimSpace(assignment.Key), Condition: strings.TrimSpace(assignment.Value)})
		return nil
	default:
		return parseErrorAt(lineNo, text, 1, spanWidth(firstToken(text)), "unexpected indented declaration")
	}
}

func parseLevelTwo(doc *Document, state *parserState, text string, lineNo int) error {
	if state.section == "schema" && state.nestedSection == "metadata" {
		return parseMetadataField(doc, text, lineNo)
	}
	if state.section != "method" || state.currentMethod == nil || state.nestedSection == "" {
		return parseErrorAt(lineNo, text, 1, spanWidth(firstToken(text)), "nested declarations are only valid under @metadata, @args_input, or @args_output")
	}
	assignment, err := parseAssignment(text)
	if err != nil {
		return parseWrapAt(lineNo, text, 1, spanWidth(firstToken(text)), "invalid nested declaration", err)
	}
	switch assignment.Key {
	case "decl_args":
		params, err := parseParameters(assignment.Value)
		if err != nil {
			return parseWrapAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "invalid decl_args value", err)
		}
		if state.nestedSection == "input" {
			state.currentMethod.InputArgs = params
		} else {
			state.currentMethod.OutputArgs = params
		}
		return nil
	case "decl_format":
		if state.nestedSection != "output" {
			return parseErrorAt(lineNo, text, assignment.KeyColumn, assignment.KeyWidth, "decl_format is only allowed under @args_output")
		}
		state.currentMethod.OutputFormat, err = ParsePayloadFormat(assignment.Value)
		if err != nil {
			return parseWrapAt(lineNo, text, assignment.ValueColumn, assignment.ValueWidth, "invalid decl_format value", err)
		}
		return nil
	default:
		return parseErrorAt(lineNo, text, assignment.KeyColumn, assignment.KeyWidth, "unknown nested declaration keyword; expected decl_args or decl_format")
	}
}

func parseMetadataField(doc *Document, text string, lineNo int) error {
	assignment, err := parseAssignment(text)
	if err != nil {
		return parseWrapAt(lineNo, text, 1, spanWidth(firstToken(text)), "invalid metadata entry", err)
	}
	value := strings.TrimSpace(assignment.Value)
	switch assignment.Key {
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
		return parseErrorAt(lineNo, text, assignment.KeyColumn, assignment.KeyWidth, "unknown metadata field; expected description, tags, display_name, or category")
	}
}

type assignment struct {
	Key         string
	Value       string
	KeyColumn   int
	KeyWidth    int
	ValueColumn int
	ValueWidth  int
}

func parseAssignment(text string) (assignment, error) {
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return assignment{}, platform.New(platform.CodeParse, "expected key: value")
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return assignment{}, platform.New(platform.CodeParse, "assignment key is required")
	}
	keyColumn := strings.Index(text, key) + 1
	valueColumn := len(parts[0]) + 2
	if value != "" {
		valueColumn = strings.Index(text, value) + 1
	}
	return assignment{
		Key:         key,
		Value:       value,
		KeyColumn:   maxInt(1, keyColumn),
		KeyWidth:    spanWidth(key),
		ValueColumn: maxInt(1, valueColumn),
		ValueWidth:  spanWidth(value),
	}, nil
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
	offset := 0
	for _, part := range parts {
		pair := strings.TrimSpace(part)
		leading := len(part) - len(strings.TrimLeft(part, " "))
		pairColumn := offset + leading + 1
		segments := strings.Split(pair, ".")
		if len(segments) != 2 {
			return nil, &spanError{Column: pairColumn, Width: spanWidth(pair), Message: fmt.Sprintf("invalid parameter declaration %q", pair)}
		}
		name := strings.TrimSpace(segments[0])
		typeToken := strings.TrimSpace(segments[1])
		argType, err := ParseArgType(typeToken)
		if err != nil {
			typeColumn := pairColumn + strings.Index(pair, typeToken)
			return nil, &spanError{Column: typeColumn, Width: spanWidth(typeToken), Message: parseDetail(err)}
		}
		params = append(params, Parameter{Name: name, Type: argType})
		offset += len(part) + 1
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

type spanError struct {
	Column  int
	Width   int
	Message string
}

func (e *spanError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func parseError(lineNo int, source string, message string) error {
	return parseErrorAt(lineNo, source, 1, spanWidth(firstToken(source)), message)
}

func parseErrorAt(lineNo int, source string, column int, width int, message string) error {
	return platform.New(platform.CodeParse, lineMessage(lineNo, source, column, width, message))
}

func parseWrap(lineNo int, source string, message string, err error) error {
	return parseWrapAt(lineNo, source, 1, 1, message, err)
}

func parseWrapAt(lineNo int, source string, column int, width int, message string, err error) error {
	detail := parseDetail(err)
	var span *spanError
	if errors.As(err, &span) {
		column += span.Column - 1
		width = span.Width
	}
	if detail == "" {
		return parseErrorAt(lineNo, source, column, width, message)
	}
	return platform.New(platform.CodeParse, fmt.Sprintf("%s: %s", lineMessage(lineNo, source, column, width, message), detail))
}

func parseDetail(err error) string {
	if err == nil {
		return ""
	}
	if coded := platform.As(err); coded != nil {
		return coded.Message
	}
	return err.Error()
}

func lineMessage(lineNo int, source string, column int, width int, message string) string {
	line := strings.TrimRight(stripComments(source), " ")
	if line == "" {
		line = source
	}
	if line == "" {
		return fmt.Sprintf("line %d, column %d: %s", lineNo, column, message)
	}
	column = maxInt(1, column)
	width = maxInt(1, width)
	return fmt.Sprintf("line %d, column %d: %s; source: %q; pointer: %q", lineNo, column, message, line, pointerLine(line, column, width))
}

func pointerLine(source string, column int, width int) string {
	if column < 1 {
		column = 1
	}
	if width < 1 {
		width = 1
	}
	lineLen := len(source)
	if lineLen == 0 {
		return "^"
	}
	if column > lineLen {
		column = lineLen
	}
	maxWidth := lineLen - column + 1
	if maxWidth < 1 {
		maxWidth = 1
	}
	if width > maxWidth {
		width = maxWidth
	}
	return strings.Repeat(" ", column-1) + strings.Repeat("^", width)
}

func firstToken(text string) string {
	if text == "" {
		return ""
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func spanWidth(token string) int {
	if token == "" {
		return 1
	}
	return len(token)
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
