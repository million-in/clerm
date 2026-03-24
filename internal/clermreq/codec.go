package clermreq

import (
	"bytes"
	"encoding/json"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermwire"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

var magic = [4]byte{'C', 'L', 'R', 'M'}

const (
	legacyFormatVersion uint16 = 1
	formatVersion       uint16 = 2
)

type Argument struct {
	Name string          `json:"name"`
	Type schema.ArgType  `json:"type"`
	Raw  json.RawMessage `json:"raw"`
}

type Request struct {
	Method                 string     `json:"method"`
	Arguments              []Argument `json:"arguments"`
	CapabilityRaw          []byte     `json:"-"`
	validatedCapabilityRaw []byte
}

func Magic() [4]byte {
	return magic
}

func Build(method schema.Method, payloadJSON []byte) (*Request, error) {
	values, err := clermwire.BuildValues(method.InputArgs, payloadJSON, "request argument")
	if err != nil {
		return nil, err
	}
	request := &Request{Method: method.Reference.Raw, Arguments: make([]Argument, len(values))}
	for i, value := range values {
		request.Arguments[i] = Argument{Name: value.Name, Type: value.Type, Raw: value.Raw}
	}
	return request, nil
}

func (r *Request) ValidateAgainst(method schema.Method) error {
	if r == nil {
		return platform.New(platform.CodeInvalidArgument, "request is required")
	}
	if r.Method != method.Reference.Raw {
		return platform.New(platform.CodeValidation, "request method does not match schema method")
	}
	if len(r.Arguments) != len(method.InputArgs) {
		return platform.New(platform.CodeValidation, "request argument count does not match schema definition")
	}
	for i, arg := range r.Arguments {
		expected := method.InputArgs[i]
		if arg.Name != expected.Name {
			return platform.New(platform.CodeValidation, "request argument order mismatch for "+expected.Name)
		}
		if arg.Type != expected.Type {
			return platform.New(platform.CodeValidation, "request argument type mismatch for "+arg.Name)
		}
		if err := clermwire.ValidateValue(arg.Type, arg.Raw); err != nil {
			return platform.Wrap(platform.CodeValidation, err, "invalid request argument "+arg.Name)
		}
	}
	return nil
}

func (r *Request) AsMap() (map[string]any, error) {
	out := make(map[string]any, len(r.Arguments))
	for _, arg := range r.Arguments {
		value, err := clermwire.DecodeValue(arg.Type, arg.Raw)
		if err != nil {
			return nil, err
		}
		out[arg.Name] = value
	}
	return out, nil
}

// SetCapabilityRaw validates and stores an owned copy of the attached token.
func (r *Request) SetCapabilityRaw(token []byte) error {
	if r == nil {
		return platform.New(platform.CodeInvalidArgument, "request is required")
	}
	if len(token) == 0 {
		r.CapabilityRaw = nil
		r.validatedCapabilityRaw = nil
		return nil
	}
	if _, err := capability.Decode(token); err != nil {
		return platform.Wrap(platform.CodeValidation, err, "invalid capability token on request")
	}
	r.CapabilityRaw = append(r.CapabilityRaw[:0], token...)
	r.validatedCapabilityRaw = append(r.validatedCapabilityRaw[:0], r.CapabilityRaw...)
	return nil
}

func Encode(request *Request) ([]byte, error) {
	if request == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "request is required")
	}
	if err := request.validateCapabilityRaw(); err != nil {
		return nil, err
	}
	buf := make([]byte, 0, encodedSize(request))
	buf = append(buf, magic[:]...)
	buf = appendUint16(buf, formatVersion)
	buf = appendString(buf, request.Method)
	buf = appendUint16(buf, uint16(len(request.Arguments)))
	for _, arg := range request.Arguments {
		buf = appendString(buf, arg.Name)
		buf = append(buf, byte(arg.Type))
		buf = appendBytes(buf, arg.Raw)
	}
	if len(request.CapabilityRaw) == 0 {
		buf = append(buf, 0)
		return buf, nil
	}
	buf = append(buf, 1)
	buf = appendBytes(buf, request.CapabilityRaw)
	return buf, nil
}

func Decode(data []byte) (*Request, error) {
	dec := newDecoder(data)
	header, err := dec.readFixed(4)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read request magic")
	}
	if !bytes.Equal(header, magic[:]) {
		return nil, platform.New(platform.CodeValidation, "invalid request magic header")
	}
	version, err := dec.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read request version")
	}
	if version != legacyFormatVersion && version != formatVersion {
		return nil, platform.New(platform.CodeValidation, "unsupported request format version")
	}
	request := &Request{}
	if request.Method, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read request method")
	}
	count, err := dec.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read request argument count")
	}
	request.Arguments = make([]Argument, count)
	for i := 0; i < int(count); i++ {
		name, err := dec.readString()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read request argument name")
		}
		typeByte, err := dec.readByte()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read request argument type")
		}
		raw, err := dec.readBytes()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read request argument payload")
		}
		request.Arguments[i] = Argument{Name: name, Type: schema.ArgType(typeByte), Raw: raw}
	}
	if version >= formatVersion {
		hasCapability, err := dec.readByte()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read request capability marker")
		}
		if hasCapability > 1 {
			return nil, platform.New(platform.CodeValidation, "invalid request capability marker")
		}
		if hasCapability == 1 {
			token, err := dec.readBytes()
			if err != nil {
				return nil, platform.Wrap(platform.CodeIO, err, "read request capability token")
			}
			request.CapabilityRaw = token
			request.validatedCapabilityRaw = nil
		}
	}
	if dec.remaining() != 0 {
		return nil, platform.New(platform.CodeValidation, "unexpected trailing bytes in request")
	}
	return request, nil
}

func IsEncoded(data []byte) bool {
	return len(data) >= len(magic) && bytes.Equal(data[:len(magic)], magic[:])
}

func encodedSize(request *Request) int {
	size := len(magic) + 2 + stringSize(request.Method) + 2
	for _, arg := range request.Arguments {
		size += stringSize(arg.Name) + 1 + 4 + len(arg.Raw)
	}
	size++
	if len(request.CapabilityRaw) > 0 {
		size += 4 + len(request.CapabilityRaw)
	}
	return size
}

func (r *Request) validateCapabilityRaw() error {
	if r == nil {
		return platform.New(platform.CodeInvalidArgument, "request is required")
	}
	if len(r.CapabilityRaw) == 0 {
		r.validatedCapabilityRaw = nil
		return nil
	}
	if bytes.Equal(r.CapabilityRaw, r.validatedCapabilityRaw) {
		return nil
	}
	if _, err := capability.Decode(r.CapabilityRaw); err != nil {
		return platform.Wrap(platform.CodeValidation, err, "invalid capability token on request")
	}
	r.validatedCapabilityRaw = append(r.validatedCapabilityRaw[:0], r.CapabilityRaw...)
	return nil
}

func appendUint16(dst []byte, value uint16) []byte {
	return append(dst, byte(value>>8), byte(value))
}

func appendUint32(dst []byte, value uint32) []byte {
	return append(dst, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func appendString(dst []byte, value string) []byte {
	dst = appendUint16(dst, uint16(len(value)))
	return append(dst, value...)
}

func appendBytes(dst []byte, value []byte) []byte {
	dst = appendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

func stringSize(value string) int {
	return 2 + len(value)
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

func (d *decoder) readUint32() (uint32, error) {
	if d.off+4 > len(d.data) {
		return 0, platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := uint32(d.data[d.off])<<24 | uint32(d.data[d.off+1])<<16 | uint32(d.data[d.off+2])<<8 | uint32(d.data[d.off+3])
	d.off += 4
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

func (d *decoder) readBytes() (json.RawMessage, error) {
	length, err := d.readUint32()
	if err != nil {
		return nil, err
	}
	if d.off+int(length) > len(d.data) {
		return nil, platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := json.RawMessage(append([]byte(nil), d.data[d.off:d.off+int(length)]...))
	d.off += int(length)
	return value, nil
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
