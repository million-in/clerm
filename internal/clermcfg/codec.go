package clermcfg

import (
	"bytes"
	"unsafe"

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
		buf = appendCount(buf, method.InputCount)
		buf = appendParameters(buf, method.InputArgs)
		buf = appendCount(buf, method.OutputCount)
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
	doc    schema.Document
	params []schema.Parameter
}

func (d *Decoder) DecodeCodec(data []byte) (*schema.Document, error) {
	plan, err := scanDecodePlan(data)
	if err != nil {
		return nil, err
	}
	if cap(d.doc.Services) < plan.serviceCount {
		d.doc.Services = make([]schema.ServiceRef, plan.serviceCount)
	} else {
		d.doc.Services = d.doc.Services[:plan.serviceCount]
	}
	if cap(d.doc.Methods) < plan.methodCount {
		d.doc.Methods = make([]schema.Method, plan.methodCount)
	} else {
		d.doc.Methods = d.doc.Methods[:plan.methodCount]
	}
	if cap(d.doc.Relations) < plan.relationCount {
		d.doc.Relations = make([]schema.RelationRule, plan.relationCount)
	} else {
		d.doc.Relations = d.doc.Relations[:plan.relationCount]
	}
	if cap(d.params) < plan.parameterCount {
		d.params = make([]schema.Parameter, plan.parameterCount)
	} else {
		d.params = d.params[:plan.parameterCount]
	}
	if err := decodeIntoWithPool(&d.doc, d.params, data, plan); err != nil {
		return nil, err
	}
	return &d.doc, nil
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

type decodePlan struct {
	serviceCount   int
	methodCount    int
	relationCount  int
	parameterCount int
}

func scanDecodePlan(data []byte) (decodePlan, error) {
	dec := newDecoder(data)
	header, err := dec.readFixed(4)
	if err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read config magic")
	}
	if !bytes.Equal(header, magic[:]) {
		return decodePlan{}, platform.New(platform.CodeValidation, "invalid config magic header")
	}
	version, err := dec.readUint16()
	if err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read config version")
	}
	if version != formatVersionV1 && version != formatVersionV2 {
		return decodePlan{}, platform.New(platform.CodeValidation, "unsupported config format version")
	}
	if err := dec.skipString(); err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read schema name")
	}
	if err := dec.skipString(); err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read relations name")
	}
	if version >= formatVersionV2 {
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read metadata description")
		}
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read metadata display name")
		}
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read metadata category")
		}
		if err := dec.skipStringList(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read metadata tags")
		}
	}
	if err := dec.skipString(); err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read route")
	}
	serviceCount, err := dec.readUint16()
	if err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read service count")
	}
	for i := 0; i < int(serviceCount); i++ {
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read service reference")
		}
	}
	methodCount, err := dec.readUint16()
	if err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read method count")
	}
	paramCount := 0
	for i := 0; i < int(methodCount); i++ {
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read method reference")
		}
		if _, err := dec.readByte(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read execution mode")
		}
		if _, err := dec.readCount(); err != nil {
			return decodePlan{}, err
		}
		count, err := dec.skipParameters()
		if err != nil {
			return decodePlan{}, err
		}
		paramCount += count
		if _, err := dec.readCount(); err != nil {
			return decodePlan{}, err
		}
		count, err = dec.skipParameters()
		if err != nil {
			return decodePlan{}, err
		}
		paramCount += count
		if _, err := dec.readByte(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read output format")
		}
	}
	relationCount, err := dec.readUint16()
	if err != nil {
		return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read relation count")
	}
	for i := 0; i < int(relationCount); i++ {
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read relation name")
		}
		if err := dec.skipString(); err != nil {
			return decodePlan{}, platform.Wrap(platform.CodeIO, err, "read relation condition")
		}
	}
	if dec.remaining() != 0 {
		return decodePlan{}, platform.New(platform.CodeValidation, "unexpected trailing bytes in config")
	}
	return decodePlan{
		serviceCount:   int(serviceCount),
		methodCount:    int(methodCount),
		relationCount:  int(relationCount),
		parameterCount: paramCount,
	}, nil
}

func decodeIntoWithPool(doc *schema.Document, pool []schema.Parameter, data []byte, plan decodePlan) error {
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
	if int(serviceCount) != plan.serviceCount {
		return platform.New(platform.CodeValidation, "config decode plan mismatch for services")
	}
	for i := 0; i < int(serviceCount); i++ {
		raw, err := dec.readString()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read service reference")
		}
		ref, err := schema.ParseServiceRef(raw)
		if err != nil {
			return err
		}
		doc.Services[i] = ref
	}
	methodCount, err := dec.readUint16()
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read method count")
	}
	if int(methodCount) != plan.methodCount {
		return platform.New(platform.CodeValidation, "config decode plan mismatch for methods")
	}
	offset := 0
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
		inputArgs, nextOffset, err := dec.readParametersInto(pool, offset)
		if err != nil {
			return err
		}
		offset = nextOffset
		outputCount, err := dec.readCount()
		if err != nil {
			return err
		}
		outputArgs, nextOffset, err := dec.readParametersInto(pool, offset)
		if err != nil {
			return err
		}
		offset = nextOffset
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
	if int(relationCount) != plan.relationCount {
		return platform.New(platform.CodeValidation, "config decode plan mismatch for relations")
	}
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

func appendCount(dst []byte, count int) []byte {
	if count < 0 {
		return appendUint16(dst, inferredCount)
	}
	return appendUint16(dst, uint16(count))
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

func (d *decoder) readParametersInto(pool []schema.Parameter, offset int) ([]schema.Parameter, int, error) {
	count, err := d.readUint16()
	if err != nil {
		return nil, offset, platform.Wrap(platform.CodeIO, err, "read parameter count")
	}
	if offset+int(count) > len(pool) {
		return nil, offset, platform.New(platform.CodeValidation, "config parameter pool overflow")
	}
	params := pool[offset : offset+int(count)]
	for i := 0; i < int(count); i++ {
		name, err := d.readString()
		if err != nil {
			return nil, offset, platform.Wrap(platform.CodeIO, err, "read parameter name")
		}
		typeByte, err := d.readByte()
		if err != nil {
			return nil, offset, platform.Wrap(platform.CodeIO, err, "read parameter type")
		}
		params[i] = schema.Parameter{Name: name, Type: schema.ArgType(typeByte)}
	}
	return params, offset + int(count), nil
}

func (d *decoder) skipString() error {
	length, err := d.readUint16()
	if err != nil {
		return err
	}
	if d.off+int(length) > len(d.data) {
		return platform.New(platform.CodeIO, "unexpected end of input")
	}
	d.off += int(length)
	return nil
}

func (d *decoder) skipStringList() error {
	count, err := d.readUint16()
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read string list count")
	}
	for i := 0; i < int(count); i++ {
		if err := d.skipString(); err != nil {
			return platform.Wrap(platform.CodeIO, err, "read string list value")
		}
	}
	return nil
}

func (d *decoder) skipParameters() (int, error) {
	count, err := d.readUint16()
	if err != nil {
		return 0, platform.Wrap(platform.CodeIO, err, "read parameter count")
	}
	for i := 0; i < int(count); i++ {
		if err := d.skipString(); err != nil {
			return 0, platform.Wrap(platform.CodeIO, err, "read parameter name")
		}
		if _, err := d.readByte(); err != nil {
			return 0, platform.Wrap(platform.CodeIO, err, "read parameter type")
		}
	}
	return int(count), nil
}

func (d *decoder) remaining() int {
	return len(d.data) - d.off
}

func bytesToString(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(value), len(value))
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
