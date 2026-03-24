package clermcfg

import (
	"bytes"

	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

var magic = [4]byte{'C', 'L', 'R', 'C'}

const (
	formatVersionV1 uint16 = 1
	formatVersionV2 uint16 = 2
	inferredCount   uint16 = 0xFFFF
)

func Magic() [4]byte {
	return magic
}

func Encode(doc *schema.Document) ([]byte, error) {
	if doc == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "document is required")
	}
	buf := make([]byte, 0, encodedSize(doc))
	buf = append(buf, magic[:]...)
	buf = appendUint16(buf, formatVersionV2)
	buf = appendString(buf, doc.Name)
	buf = appendString(buf, doc.RelationsName)
	buf = appendString(buf, doc.Metadata.Description)
	buf = appendString(buf, doc.Metadata.DisplayName)
	buf = appendString(buf, doc.Metadata.Category)
	buf = appendStringList(buf, doc.Metadata.Tags)
	buf = appendString(buf, doc.Route)
	buf = appendUint16(buf, uint16(len(doc.Services)))
	for _, service := range doc.Services {
		buf = appendString(buf, service.Raw)
	}
	buf = appendUint16(buf, uint16(len(doc.Methods)))
	for _, method := range doc.Methods {
		buf = appendString(buf, method.Reference.Raw)
		buf = append(buf, byte(method.Execution))
		var err error
		buf, err = appendCount(buf, method.InputCount)
		if err != nil {
			return nil, platform.Wrap(platform.CodeValidation, err, "encode input count for "+method.Reference.Raw)
		}
		buf = appendParameters(buf, method.InputArgs)
		buf, err = appendCount(buf, method.OutputCount)
		if err != nil {
			return nil, platform.Wrap(platform.CodeValidation, err, "encode output count for "+method.Reference.Raw)
		}
		buf = appendParameters(buf, method.OutputArgs)
		buf = append(buf, byte(method.OutputFormat))
	}
	buf = appendUint16(buf, uint16(len(doc.Relations)))
	for _, relation := range doc.Relations {
		buf = appendString(buf, relation.Name)
		buf = appendString(buf, relation.Condition)
	}
	return buf, nil
}

func Decode(data []byte) (*schema.Document, error) {
	doc, err := DecodeCodec(data)
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

func DecodeCodec(data []byte) (*schema.Document, error) {
	doc := &schema.Document{}
	if err := decodeInto(doc, data); err != nil {
		return nil, err
	}
	return doc, nil
}

func DecodeInto(doc *schema.Document, data []byte) error {
	if err := DecodeCodecInto(doc, data); err != nil {
		return err
	}
	return doc.Validate()
}

func DecodeCodecInto(doc *schema.Document, data []byte) error {
	if doc == nil {
		return platform.New(platform.CodeInvalidArgument, "document is required")
	}
	return decodeInto(doc, data)
}

func decodeInto(doc *schema.Document, data []byte) error {
	dec := newDecoder(data)
	header, err := dec.readFixed(4)
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read config magic")
	}
	if !bytes.Equal(header, magic[:]) {
		return platform.New(platform.CodeValidation, "invalid config magic header")
	}
	version, err := dec.readUint16()
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read config version")
	}
	if version != formatVersionV1 && version != formatVersionV2 {
		return platform.New(platform.CodeValidation, "unsupported config format version")
	}

	if doc.Name, err = dec.readString(); err != nil {
		return platform.Wrap(platform.CodeIO, err, "read schema name")
	}
	if doc.RelationsName, err = dec.readString(); err != nil {
		return platform.Wrap(platform.CodeIO, err, "read relations name")
	}
	if version >= formatVersionV2 {
		if doc.Metadata.Description, err = dec.readString(); err != nil {
			return platform.Wrap(platform.CodeIO, err, "read metadata description")
		}
		if doc.Metadata.DisplayName, err = dec.readString(); err != nil {
			return platform.Wrap(platform.CodeIO, err, "read metadata display name")
		}
		if doc.Metadata.Category, err = dec.readString(); err != nil {
			return platform.Wrap(platform.CodeIO, err, "read metadata category")
		}
		if doc.Metadata.Tags, err = dec.readStringList(); err != nil {
			return platform.Wrap(platform.CodeIO, err, "read metadata tags")
		}
	} else {
		doc.Metadata = schema.Metadata{}
	}
	if doc.Route, err = dec.readString(); err != nil {
		return platform.Wrap(platform.CodeIO, err, "read route")
	}
	serviceCount, err := dec.readUint16()
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read service count")
	}
	doc.Services = resizeServices(doc.Services, int(serviceCount))
	for i := 0; i < int(serviceCount); i++ {
		raw, err := dec.readString()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read service reference")
		}
		service, err := schema.ParseServiceRef(raw)
		if err != nil {
			return err
		}
		doc.Services[i] = service
	}
	methodCount, err := dec.readUint16()
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read method count")
	}
	doc.Methods = resizeMethods(doc.Methods, int(methodCount))
	for i := 0; i < int(methodCount); i++ {
		raw, err := dec.readString()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read method reference")
		}
		ref, err := schema.ParseServiceRef(raw)
		if err != nil {
			return err
		}
		execByte, err := dec.readByte()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read execution mode")
		}
		inputCount, err := dec.readCount()
		if err != nil {
			return err
		}
		inputArgs, err := dec.readParameters()
		if err != nil {
			return err
		}
		outputCount, err := dec.readCount()
		if err != nil {
			return err
		}
		outputArgs, err := dec.readParameters()
		if err != nil {
			return err
		}
		formatByte, err := dec.readByte()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read output format")
		}
		doc.Methods[i] = schema.Method{
			Reference:    ref,
			Execution:    schema.ExecutionMode(execByte),
			InputCount:   inputCount,
			InputArgs:    inputArgs,
			OutputCount:  outputCount,
			OutputArgs:   outputArgs,
			OutputFormat: schema.PayloadFormat(formatByte),
		}
	}
	relationCount, err := dec.readUint16()
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read relation count")
	}
	doc.Relations = resizeRelations(doc.Relations, int(relationCount))
	for i := 0; i < int(relationCount); i++ {
		name, err := dec.readString()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read relation name")
		}
		condition, err := dec.readString()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read relation condition")
		}
		doc.Relations[i] = schema.RelationRule{Name: name, Condition: condition}
	}
	if dec.remaining() != 0 {
		return platform.New(platform.CodeValidation, "unexpected trailing bytes in config")
	}
	return nil
}

type Decoder struct {
	doc schema.Document
}

func (d *Decoder) DecodeCodec(data []byte) (*schema.Document, error) {
	if err := decodeInto(&d.doc, data); err != nil {
		return nil, err
	}
	return cloneDocument(&d.doc), nil
}

func (d *Decoder) Decode(data []byte) (*schema.Document, error) {
	doc, err := d.DecodeCodec(data)
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

func IsEncoded(data []byte) bool {
	return len(data) >= len(magic) && bytes.Equal(data[:len(magic)], magic[:])
}

func encodedSize(doc *schema.Document) int {
	size := len(magic) + 2
	size += stringSize(doc.Name)
	size += stringSize(doc.RelationsName)
	size += stringSize(doc.Metadata.Description)
	size += stringSize(doc.Metadata.DisplayName)
	size += stringSize(doc.Metadata.Category)
	size += stringListSize(doc.Metadata.Tags)
	size += stringSize(doc.Route)
	size += 2
	for _, service := range doc.Services {
		size += stringSize(service.Raw)
	}
	size += 2
	for _, method := range doc.Methods {
		size += stringSize(method.Reference.Raw)
		size++
		size += 2
		size += parametersSize(method.InputArgs)
		size += 2
		size += parametersSize(method.OutputArgs)
		size++
	}
	size += 2
	for _, relation := range doc.Relations {
		size += stringSize(relation.Name)
		size += stringSize(relation.Condition)
	}
	return size
}

func parametersSize(params []schema.Parameter) int {
	size := 2
	for _, param := range params {
		size += stringSize(param.Name) + 1
	}
	return size
}

func appendParameters(dst []byte, params []schema.Parameter) []byte {
	dst = appendUint16(dst, uint16(len(params)))
	for _, param := range params {
		dst = appendString(dst, param.Name)
		dst = append(dst, byte(param.Type))
	}
	return dst
}

func appendStringList(dst []byte, values []string) []byte {
	dst = appendUint16(dst, uint16(len(values)))
	for _, value := range values {
		dst = appendString(dst, value)
	}
	return dst
}

func appendUint16(dst []byte, value uint16) []byte {
	return append(dst, byte(value>>8), byte(value))
}

func appendString(dst []byte, value string) []byte {
	dst = appendUint16(dst, uint16(len(value)))
	return append(dst, value...)
}

func appendCount(dst []byte, count int) ([]byte, error) {
	switch {
	case count == int(inferredCount):
		return nil, platform.New(platform.CodeValidation, "declared count 65535 is reserved for inferred counts")
	case count < -1:
		return nil, platform.New(platform.CodeValidation, "declared count must be -1 or greater")
	case count > int(^uint16(0))-1:
		return nil, platform.New(platform.CodeValidation, "declared count exceeds wire format limit")
	}
	if count < 0 {
		return appendUint16(dst, inferredCount), nil
	}
	return appendUint16(dst, uint16(count)), nil
}

func stringSize(value string) int {
	return 2 + len(value)
}

func stringListSize(values []string) int {
	size := 2
	for _, value := range values {
		size += stringSize(value)
	}
	return size
}

type decoder struct {
	data []byte
	off  int
}

func newDecoder(data []byte) *decoder {
	return &decoder{data: data}
}

func (d *decoder) readFixed(n int) ([]byte, error) {
	if d.off+n > len(d.data) {
		return nil, platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := d.data[d.off : d.off+n]
	d.off += n
	return value, nil
}

func (d *decoder) readByte() (byte, error) {
	if d.off >= len(d.data) {
		return 0, platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := d.data[d.off]
	d.off++
	return value, nil
}

func (d *decoder) readUint16() (uint16, error) {
	if d.off+2 > len(d.data) {
		return 0, platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := uint16(d.data[d.off])<<8 | uint16(d.data[d.off+1])
	d.off += 2
	return value, nil
}

func (d *decoder) readString() (string, error) {
	length, err := d.readUint16()
	if err != nil {
		return "", err
	}
	if d.off+int(length) > len(d.data) {
		return "", platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := bytesToString(d.data[d.off : d.off+int(length)])
	d.off += int(length)
	return value, nil
}

func (d *decoder) readCount() (int, error) {
	value, err := d.readUint16()
	if err != nil {
		return 0, platform.Wrap(platform.CodeIO, err, "read declared count")
	}
	if value == inferredCount {
		return -1, nil
	}
	return int(value), nil
}

func (d *decoder) readStringList() ([]string, error) {
	count, err := d.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read string list count")
	}
	values := make([]string, int(count))
	for i := 0; i < int(count); i++ {
		value, err := d.readString()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read string list value")
		}
		values[i] = value
	}
	return values, nil
}

func (d *decoder) readParameters() ([]schema.Parameter, error) {
	count, err := d.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read parameter count")
	}
	params := make([]schema.Parameter, count)
	for i := 0; i < int(count); i++ {
		name, err := d.readString()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read parameter name")
		}
		typeByte, err := d.readByte()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read parameter type")
		}
		params[i] = schema.Parameter{Name: name, Type: schema.ArgType(typeByte)}
	}
	return params, nil
}

func (d *decoder) remaining() int {
	return len(d.data) - d.off
}

func bytesToString(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return string(value)
}

func cloneDocument(doc *schema.Document) *schema.Document {
	if doc == nil {
		return nil
	}
	cloned := &schema.Document{
		Name:          doc.Name,
		RelationsName: doc.RelationsName,
		Metadata: schema.Metadata{
			Description: doc.Metadata.Description,
			Tags:        append([]string(nil), doc.Metadata.Tags...),
			DisplayName: doc.Metadata.DisplayName,
			Category:    doc.Metadata.Category,
		},
		Route:     doc.Route,
		Services:  append([]schema.ServiceRef(nil), doc.Services...),
		Relations: append([]schema.RelationRule(nil), doc.Relations...),
	}
	cloned.Methods = make([]schema.Method, len(doc.Methods))
	for i, method := range doc.Methods {
		clonedMethod := method
		clonedMethod.InputArgs = append([]schema.Parameter(nil), method.InputArgs...)
		clonedMethod.OutputArgs = append([]schema.Parameter(nil), method.OutputArgs...)
		cloned.Methods[i] = clonedMethod
	}
	return cloned
}

func resizeServices(values []schema.ServiceRef, n int) []schema.ServiceRef {
	if cap(values) < n {
		return make([]schema.ServiceRef, n)
	}
	return values[:n]
}

func resizeMethods(values []schema.Method, n int) []schema.Method {
	if cap(values) < n {
		return make([]schema.Method, n)
	}
	return values[:n]
}

func resizeRelations(values []schema.RelationRule, n int) []schema.RelationRule {
	if cap(values) < n {
		return make([]schema.RelationRule, n)
	}
	return values[:n]
}
