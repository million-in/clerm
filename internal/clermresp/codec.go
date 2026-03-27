package clermresp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/million-in/clerm/internal/clermwire"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

var magic = [4]byte{'C', 'L', 'R', 'S'}

const formatVersion uint16 = 1

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Value struct {
	Name string          `json:"name"`
	Type schema.ArgType  `json:"type"`
	Raw  json.RawMessage `json:"raw"`
}

type Response struct {
	Method  string    `json:"method"`
	Outputs []Value   `json:"outputs,omitempty"`
	Error   ErrorBody `json:"error,omitempty"`
}

func BuildSuccess(method schema.Method, payloadJSON []byte) (*Response, error) {
	values, err := clermwire.BuildValues(method.OutputArgs, payloadJSON, "response output")
	if err != nil {
		return nil, err
	}
	return buildSuccessValues(method, values)
}

func BuildSuccessMap(method schema.Method, payload map[string]any) (*Response, error) {
	values, err := clermwire.BuildValuesFromMap(method.OutputArgs, payload, "response output")
	if err != nil {
		return nil, err
	}
	return buildSuccessValues(method, values)
}

func BuildSuccessValues(method schema.Method, outputs []Value) (*Response, error) {
	if method.Reference.Raw == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "response method is required")
	}
	values := make([]clermwire.Value, len(outputs))
	for i, output := range outputs {
		values[i] = clermwire.Value{Name: output.Name, Type: output.Type, Raw: output.Raw}
	}
	if err := clermwire.ValidateValues(values, method.OutputArgs, "response output"); err != nil {
		return nil, err
	}
	response := &Response{Method: method.Reference.Raw, Outputs: make([]Value, len(outputs))}
	copy(response.Outputs, outputs)
	return response, nil
}

func buildSuccessValues(method schema.Method, values []clermwire.Value) (*Response, error) {
	response := &Response{Method: method.Reference.Raw, Outputs: make([]Value, len(values))}
	for i, value := range values {
		response.Outputs[i] = Value{Name: value.Name, Type: value.Type, Raw: value.Raw}
	}
	return response, nil
}

func BuildError(method schema.Method, code string, message string) (*Response, error) {
	if method.Reference.Raw == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "response method is required")
	}
	if code == "" {
		code = string(platform.CodeInternal)
	}
	return &Response{Method: method.Reference.Raw, Error: ErrorBody{Code: code, Message: message}}, nil
}

func (r *Response) ValidateAgainst(method schema.Method) error {
	if r == nil {
		return platform.New(platform.CodeInvalidArgument, "response is required")
	}
	if r.Method != method.Reference.Raw {
		return platform.New(platform.CodeValidation, "response method does not match schema method")
	}
	if r.Error.Code != "" || r.Error.Message != "" {
		return nil
	}
	if len(r.Outputs) != len(method.OutputArgs) {
		return platform.New(platform.CodeValidation, "response output count does not match schema definition")
	}
	for i, output := range r.Outputs {
		expected := method.OutputArgs[i]
		if output.Name != expected.Name {
			return platform.New(platform.CodeValidation, "response output order mismatch for "+expected.Name)
		}
		if output.Type != expected.Type {
			return platform.New(platform.CodeValidation, "response output type mismatch for "+output.Name)
		}
		if err := clermwire.ValidateValue(output.Type, output.Raw); err != nil {
			return platform.Wrap(platform.CodeValidation, err, "invalid response output "+output.Name)
		}
	}
	return nil
}

func (r *Response) AsMap() (map[string]any, error) {
	out := make(map[string]any, len(r.Outputs))
	for _, output := range r.Outputs {
		value, err := clermwire.DecodeValue(output.Type, output.Raw)
		if err != nil {
			return nil, err
		}
		out[output.Name] = value
	}
	return out, nil
}

func Encode(response *Response) ([]byte, error) {
	if response == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "response is required")
	}
	buf := make([]byte, 0, encodedSize(response))
	buf = append(buf, magic[:]...)
	buf = appendUint16(buf, formatVersion)
	var err error
	buf, err = appendString(buf, response.Method)
	if err != nil {
		return nil, err
	}
	if response.Error.Code != "" || response.Error.Message != "" {
		buf = append(buf, 1)
		buf, err = appendString(buf, response.Error.Code)
		if err != nil {
			return nil, err
		}
		buf, err = appendString(buf, response.Error.Message)
		if err != nil {
			return nil, err
		}
		return buf, nil
	}
	buf = append(buf, 0)
	buf = appendUint16(buf, uint16(len(response.Outputs)))
	for _, output := range response.Outputs {
		buf, err = appendString(buf, output.Name)
		if err != nil {
			return nil, err
		}
		buf = append(buf, byte(output.Type))
		buf = appendBytes(buf, output.Raw)
	}
	return buf, nil
}

func WriteTo(w io.Writer, response *Response) error {
	data, err := Encode(response)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return platform.Wrap(platform.CodeIO, err, "write response payload")
	}
	return nil
}

func Decode(data []byte) (*Response, error) {
	dec := newDecoder(data)
	header, err := dec.readFixed(4)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read response magic")
	}
	if !bytes.Equal(header, magic[:]) {
		return nil, platform.New(platform.CodeValidation, "invalid response magic header")
	}
	version, err := dec.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read response version")
	}
	if version != formatVersion {
		return nil, platform.New(platform.CodeValidation, "unsupported response format version")
	}
	response := &Response{}
	if response.Method, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read response method")
	}
	hasError, err := dec.readByte()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read response error marker")
	}
	if hasError > 1 {
		return nil, platform.New(platform.CodeValidation, "invalid response error marker")
	}
	if hasError == 1 {
		if response.Error.Code, err = dec.readString(); err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read response error code")
		}
		if response.Error.Message, err = dec.readString(); err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read response error message")
		}
	} else {
		count, err := dec.readUint16()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read response output count")
		}
		if err := dec.ensureCollectionCount(int(count), 7, "response output"); err != nil {
			return nil, err
		}
		response.Outputs = make([]Value, count)
		for i := 0; i < int(count); i++ {
			name, err := dec.readString()
			if err != nil {
				return nil, platform.Wrap(platform.CodeIO, err, "read response output name")
			}
			typeByte, err := dec.readByte()
			if err != nil {
				return nil, platform.Wrap(platform.CodeIO, err, "read response output type")
			}
			raw, err := dec.readBytes()
			if err != nil {
				return nil, platform.Wrap(platform.CodeIO, err, "read response output payload")
			}
			response.Outputs[i] = Value{Name: name, Type: schema.ArgType(typeByte), Raw: raw}
		}
	}
	if dec.remaining() != 0 {
		return nil, platform.New(platform.CodeValidation, "unexpected trailing bytes in response")
	}
	return response, nil
}

func IsEncoded(data []byte) bool {
	return len(data) >= len(magic) && bytes.Equal(data[:len(magic)], magic[:])
}

func encodedSize(response *Response) int {
	size := len(magic) + 2 + stringSize(response.Method) + 1
	if response.Error.Code != "" || response.Error.Message != "" {
		return size + stringSize(response.Error.Code) + stringSize(response.Error.Message)
	}
	size += 2
	for _, output := range response.Outputs {
		size += stringSize(output.Name) + 1 + 4 + len(output.Raw)
	}
	return size
}

func appendUint16(dst []byte, value uint16) []byte {
	return append(dst, byte(value>>8), byte(value))
}

func appendUint32(dst []byte, value uint32) []byte {
	return append(dst, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func appendString(dst []byte, value string) ([]byte, error) {
	if len(value) > 0xffff {
		return nil, platform.New(platform.CodeInvalidArgument, fmt.Sprintf("response string too large: %d", len(value)))
	}
	dst = appendUint16(dst, uint16(len(value)))
	return append(dst, value...), nil
}

func appendBytes(dst []byte, value []byte) []byte {
	dst = appendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

func writeByte(w io.Writer, value byte) error {
	var buf [1]byte
	buf[0] = value
	_, err := w.Write(buf[:])
	return err
}

func writeUint16(w io.Writer, value uint16) error {
	var buf [2]byte
	buf[0] = byte(value >> 8)
	buf[1] = byte(value)
	_, err := w.Write(buf[:])
	return err
}

func writeUint32(w io.Writer, value uint32) error {
	var buf [4]byte
	buf[0] = byte(value >> 24)
	buf[1] = byte(value >> 16)
	buf[2] = byte(value >> 8)
	buf[3] = byte(value)
	_, err := w.Write(buf[:])
	return err
}

func writeString(w io.Writer, value string) error {
	if len(value) > 0xffff {
		return platform.New(platform.CodeInvalidArgument, fmt.Sprintf("response string too large: %d", len(value)))
	}
	if err := writeUint16(w, uint16(len(value))); err != nil {
		return err
	}
	_, err := io.WriteString(w, value)
	return err
}

func writeBytes(w io.Writer, value []byte) error {
	if err := writeUint32(w, uint32(len(value))); err != nil {
		return err
	}
	_, err := w.Write(value)
	return err
}

func stringSize(value string) int {
	return 2 + len(value)
}

type decoder struct {
	data []byte
	pos  int
}

func newDecoder(data []byte) *decoder { return &decoder{data: data} }

func (d *decoder) remaining() int { return len(d.data) - d.pos }

func (d *decoder) ensureCollectionCount(count int, minBytesPerItem int, label string) error {
	if count < 0 {
		return platform.New(platform.CodeValidation, label+" count is invalid")
	}
	if count == 0 {
		return nil
	}
	if minBytesPerItem <= 0 {
		return platform.New(platform.CodeInternal, "decoder min bytes per item is invalid")
	}
	if d.remaining()/minBytesPerItem < count {
		return platform.New(platform.CodeValidation, label+" count exceeds remaining payload")
	}
	return nil
}

func (d *decoder) readFixed(size int) ([]byte, error) {
	if d.remaining() < size {
		return nil, platform.New(platform.CodeIO, "unexpected end of response payload")
	}
	start := d.pos
	d.pos += size
	return d.data[start:d.pos], nil
}

func (d *decoder) readByte() (byte, error) {
	buf, err := d.readFixed(1)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (d *decoder) readUint16() (uint16, error) {
	buf, err := d.readFixed(2)
	if err != nil {
		return 0, err
	}
	return uint16(buf[0])<<8 | uint16(buf[1]), nil
}

func (d *decoder) readUint32() (uint32, error) {
	buf, err := d.readFixed(4)
	if err != nil {
		return 0, err
	}
	return uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]), nil
}

func (d *decoder) readString() (string, error) {
	length, err := d.readUint16()
	if err != nil {
		return "", err
	}
	buf, err := d.readFixed(int(length))
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func (d *decoder) readBytes() ([]byte, error) {
	length, err := d.readUint32()
	if err != nil {
		return nil, err
	}
	value, err := d.readFixed(int(length))
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), value...), nil
}
