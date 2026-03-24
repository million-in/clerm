package capability

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/million-in/clerm/internal/platform"
)

var magic = [4]byte{'C', 'L', 'C', 'P'}

const formatVersion uint16 = 1

type Token struct {
	KeyID      string   `json:"key_id"`
	Issuer     string   `json:"issuer"`
	Subject    string   `json:"subject"`
	TokenID    string   `json:"token_id"`
	Schema     string   `json:"schema"`
	SchemaHash [32]byte `json:"-"`
	Relation   string   `json:"relation"`
	Condition  string   `json:"condition"`
	Methods    []string `json:"methods,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	IssuedAt   int64    `json:"issued_at"`
	NotBefore  int64    `json:"not_before"`
	ExpiresAt  int64    `json:"expires_at"`
	Signature  []byte   `json:"-"`
}

type InspectView struct {
	KeyID      string   `json:"key_id"`
	Issuer     string   `json:"issuer"`
	Subject    string   `json:"subject"`
	TokenID    string   `json:"token_id"`
	Schema     string   `json:"schema"`
	SchemaHash string   `json:"schema_hash"`
	Relation   string   `json:"relation"`
	Condition  string   `json:"condition"`
	Methods    []string `json:"methods,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	IssuedAt   string   `json:"issued_at"`
	NotBefore  string   `json:"not_before"`
	ExpiresAt  string   `json:"expires_at"`
	Signed     bool     `json:"signed"`
}

type IssueOptions struct {
	KeyID      string
	Issuer     string
	Subject    string
	TokenID    string
	Schema     string
	SchemaHash [32]byte
	Relation   string
	Condition  string
	// Methods is sorted and deduplicated before signing.
	Methods []string
	// Targets is sorted and deduplicated before signing.
	Targets   []string
	IssuedAt  time.Time
	NotBefore time.Time
	ExpiresAt time.Time
}

type Keyring struct {
	keys map[string]ed25519.PublicKey
}

func NewKeyring(keys map[string]ed25519.PublicKey) *Keyring {
	copyKeys := make(map[string]ed25519.PublicKey, len(keys))
	for keyID, key := range keys {
		copyKeys[keyID] = append(ed25519.PublicKey(nil), key...)
	}
	return &Keyring{keys: copyKeys}
}

func (k *Keyring) Verify(token *Token) error {
	if k == nil {
		return platform.New(platform.CodeInvalidArgument, "capability keyring is required")
	}
	if token == nil {
		return platform.New(platform.CodeInvalidArgument, "capability token is required")
	}
	if err := Validate(token); err != nil {
		return err
	}
	publicKey, ok := k.keys[token.KeyID]
	if !ok {
		return platform.New(platform.CodeValidation, "capability token key id is not trusted")
	}
	payload, err := encodePayload(token)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, token.Signature) {
		return platform.New(platform.CodeValidation, "capability token signature is invalid")
	}
	return nil
}

func Issue(opts IssueOptions, privateKey ed25519.PrivateKey) (*Token, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, platform.New(platform.CodeInvalidArgument, "ed25519 private key is invalid")
	}
	issuedAt := opts.IssuedAt.UTC()
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	notBefore := opts.NotBefore.UTC()
	if notBefore.IsZero() {
		notBefore = issuedAt
	}
	expiresAt := opts.ExpiresAt.UTC()
	if expiresAt.IsZero() || !expiresAt.After(notBefore) {
		return nil, platform.New(platform.CodeInvalidArgument, "capability token expiry must be after not_before")
	}
	tokenID := strings.TrimSpace(opts.TokenID)
	if tokenID == "" {
		generated, err := newTokenID()
		if err != nil {
			return nil, err
		}
		tokenID = generated
	}
	token := &Token{
		KeyID:      strings.TrimSpace(opts.KeyID),
		Issuer:     strings.TrimSpace(opts.Issuer),
		Subject:    strings.TrimSpace(opts.Subject),
		TokenID:    tokenID,
		Schema:     strings.TrimSpace(opts.Schema),
		SchemaHash: opts.SchemaHash,
		Relation:   strings.TrimSpace(opts.Relation),
		Condition:  strings.TrimSpace(opts.Condition),
		Methods:    compactList(opts.Methods),
		Targets:    compactList(opts.Targets),
		IssuedAt:   issuedAt.Unix(),
		NotBefore:  notBefore.Unix(),
		ExpiresAt:  expiresAt.Unix(),
	}
	if err := Sign(token, privateKey); err != nil {
		return nil, err
	}
	return token, nil
}

func Sign(token *Token, privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return platform.New(platform.CodeInvalidArgument, "ed25519 private key is invalid")
	}
	if err := ValidateUnsigned(token); err != nil {
		return err
	}
	payload, err := encodePayload(token)
	if err != nil {
		return err
	}
	token.Signature = ed25519.Sign(privateKey, payload)
	return nil
}

func Validate(token *Token) error {
	if err := ValidateUnsigned(token); err != nil {
		return err
	}
	if len(token.Signature) != ed25519.SignatureSize {
		return platform.New(platform.CodeValidation, "capability token signature is invalid")
	}
	return nil
}

func ValidateUnsigned(token *Token) error {
	if token == nil {
		return platform.New(platform.CodeInvalidArgument, "capability token is required")
	}
	if strings.TrimSpace(token.KeyID) == "" {
		return platform.New(platform.CodeValidation, "capability token key id is required")
	}
	if strings.TrimSpace(token.Issuer) == "" {
		return platform.New(platform.CodeValidation, "capability token issuer is required")
	}
	if strings.TrimSpace(token.Subject) == "" {
		return platform.New(platform.CodeValidation, "capability token subject is required")
	}
	if strings.TrimSpace(token.TokenID) == "" {
		return platform.New(platform.CodeValidation, "capability token id is required")
	}
	if strings.TrimSpace(token.Schema) == "" {
		return platform.New(platform.CodeValidation, "capability token schema is required")
	}
	if isZeroHash(token.SchemaHash) {
		return platform.New(platform.CodeValidation, "capability token schema fingerprint is required")
	}
	if strings.TrimSpace(token.Relation) == "" {
		return platform.New(platform.CodeValidation, "capability token relation is required")
	}
	if strings.TrimSpace(token.Condition) == "" {
		return platform.New(platform.CodeValidation, "capability token condition is required")
	}
	if token.NotBefore < token.IssuedAt {
		return platform.New(platform.CodeValidation, "capability token not_before cannot be before issued_at")
	}
	if token.ExpiresAt <= token.NotBefore {
		return platform.New(platform.CodeValidation, "capability token expiry must be after not_before")
	}
	if hasDuplicates(token.Methods) {
		return platform.New(platform.CodeValidation, "capability token methods contain duplicates")
	}
	if hasDuplicates(token.Targets) {
		return platform.New(platform.CodeValidation, "capability token targets contain duplicates")
	}
	return nil
}

// AssertTimeWindow checks only the temporal claims on a token.
// It does not verify the cryptographic signature. Call Keyring.Verify first.
func AssertTimeWindow(token *Token, now time.Time, skew time.Duration) error {
	if err := Validate(token); err != nil {
		return err
	}
	current := now.UTC().Unix()
	if skew < 0 {
		skew = 0
	}
	leeway := int64(skew / time.Second)
	if current+leeway < token.NotBefore {
		return platform.New(platform.CodeValidation, "capability token is not valid yet")
	}
	if current-leeway > token.ExpiresAt {
		return platform.New(platform.CodeValidation, "capability token has expired")
	}
	return nil
}

// VerifyTime checks only the temporal claims on a token.
// It does not verify the cryptographic signature. Call Keyring.Verify first.
func VerifyTime(token *Token, now time.Time, skew time.Duration) error {
	return AssertTimeWindow(token, now, skew)
}

func (t *Token) AllowsMethod(method string, relation string) bool {
	if t == nil || strings.TrimSpace(relation) != t.Relation {
		return false
	}
	if len(t.Methods) == 0 {
		return true
	}
	for _, allowed := range t.Methods {
		if allowed == method {
			return true
		}
	}
	return false
}

func (t *Token) AllowsTarget(target string) bool {
	if t == nil {
		return false
	}
	if len(t.Targets) == 0 {
		return true
	}
	for _, allowed := range t.Targets {
		if allowed == target {
			return true
		}
	}
	return false
}

func (t *Token) TTL(now time.Time) time.Duration {
	if t == nil {
		return 0
	}
	delta := time.Unix(t.ExpiresAt, 0).Sub(now.UTC())
	if delta < 0 {
		return 0
	}
	return delta
}

func (t *Token) InspectView() InspectView {
	view := InspectView{}
	if t == nil {
		return view
	}
	view = InspectView{
		KeyID:      t.KeyID,
		Issuer:     t.Issuer,
		Subject:    t.Subject,
		TokenID:    t.TokenID,
		Schema:     t.Schema,
		SchemaHash: hex.EncodeToString(t.SchemaHash[:]),
		Relation:   t.Relation,
		Condition:  t.Condition,
		Methods:    append([]string(nil), t.Methods...),
		Targets:    append([]string(nil), t.Targets...),
		IssuedAt:   time.Unix(t.IssuedAt, 0).UTC().Format(time.RFC3339),
		NotBefore:  time.Unix(t.NotBefore, 0).UTC().Format(time.RFC3339),
		ExpiresAt:  time.Unix(t.ExpiresAt, 0).UTC().Format(time.RFC3339),
		Signed:     len(t.Signature) == ed25519.SignatureSize,
	}
	return view
}

func EncodeText(token *Token) (string, error) {
	data, err := Encode(token)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeText(raw string) (*Token, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "capability token text is required")
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "decode capability token text")
	}
	return Decode(data)
}

func Encode(token *Token) ([]byte, error) {
	if err := Validate(token); err != nil {
		return nil, err
	}
	payload, err := encodePayload(token)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, len(payload)+2+len(token.Signature))
	buf = append(buf, payload...)
	buf = appendUint16(buf, uint16(len(token.Signature)))
	buf = append(buf, token.Signature...)
	return buf, nil
}

func Decode(data []byte) (*Token, error) {
	dec := newDecoder(data)
	token, err := decodePayload(dec)
	if err != nil {
		return nil, err
	}
	sigLen, err := dec.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability signature length")
	}
	sig, err := dec.readFixed(int(sigLen))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability signature")
	}
	token.Signature = append([]byte(nil), sig...)
	if dec.remaining() != 0 {
		return nil, platform.New(platform.CodeValidation, "unexpected trailing bytes in capability token")
	}
	if err := Validate(token); err != nil {
		return nil, err
	}
	return token, nil
}

func encodePayload(token *Token) ([]byte, error) {
	if err := ValidateUnsigned(token); err != nil {
		return nil, err
	}
	buf := make([]byte, 0, payloadSize(token))
	buf = append(buf, magic[:]...)
	buf = appendUint16(buf, formatVersion)
	buf = appendString(buf, token.KeyID)
	buf = appendString(buf, token.Issuer)
	buf = appendString(buf, token.Subject)
	buf = appendString(buf, token.TokenID)
	buf = appendString(buf, token.Schema)
	buf = append(buf, token.SchemaHash[:]...)
	buf = appendString(buf, token.Relation)
	buf = appendString(buf, token.Condition)
	buf = appendStringList(buf, token.Methods)
	buf = appendStringList(buf, token.Targets)
	buf = appendInt64(buf, token.IssuedAt)
	buf = appendInt64(buf, token.NotBefore)
	buf = appendInt64(buf, token.ExpiresAt)
	return buf, nil
}

func decodePayload(dec *decoder) (*Token, error) {
	header, err := dec.readFixed(4)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability magic")
	}
	if !bytes.Equal(header, magic[:]) {
		return nil, platform.New(platform.CodeValidation, "invalid capability token magic header")
	}
	version, err := dec.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability version")
	}
	if version != formatVersion {
		return nil, platform.New(platform.CodeValidation, "unsupported capability token format version")
	}
	token := &Token{}
	if token.KeyID, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability key id")
	}
	if token.Issuer, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability issuer")
	}
	if token.Subject, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability subject")
	}
	if token.TokenID, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability token id")
	}
	if token.Schema, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability schema")
	}
	hashBytes, err := dec.readFixed(len(token.SchemaHash))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability schema fingerprint")
	}
	copy(token.SchemaHash[:], hashBytes)
	if token.Relation, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability relation")
	}
	if token.Condition, err = dec.readString(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability condition")
	}
	if token.Methods, err = dec.readStringList(); err != nil {
		return nil, err
	}
	if token.Targets, err = dec.readStringList(); err != nil {
		return nil, err
	}
	if token.IssuedAt, err = dec.readInt64(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability issued_at")
	}
	if token.NotBefore, err = dec.readInt64(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability not_before")
	}
	if token.ExpiresAt, err = dec.readInt64(); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability expires_at")
	}
	return token, nil
}

func ReadPrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	data, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, platform.New(platform.CodeValidation, "ed25519 private key file is invalid")
	}
	return ed25519.PrivateKey(data), nil
}

func ReadPublicKeyFile(path string) (ed25519.PublicKey, error) {
	data, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PublicKeySize {
		return nil, platform.New(platform.CodeValidation, "ed25519 public key file is invalid")
	}
	return ed25519.PublicKey(data), nil
}

func WritePrivateKeyFile(path string, key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return platform.New(platform.CodeInvalidArgument, "ed25519 private key is invalid")
	}
	return writeKeyFile(path, []byte(key))
}

func WritePublicKeyFile(path string, key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize {
		return platform.New(platform.CodeInvalidArgument, "ed25519 public key is invalid")
	}
	return writeKeyFile(path, []byte(key))
}

func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func payloadSize(token *Token) int {
	size := len(magic) + 2
	size += stringSize(token.KeyID)
	size += stringSize(token.Issuer)
	size += stringSize(token.Subject)
	size += stringSize(token.TokenID)
	size += stringSize(token.Schema)
	size += len(token.SchemaHash)
	size += stringSize(token.Relation)
	size += stringSize(token.Condition)
	size += stringListSize(token.Methods)
	size += stringListSize(token.Targets)
	size += 8 + 8 + 8
	return size
}

func compactList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasDuplicates(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func isZeroHash(hash [32]byte) bool {
	for _, b := range hash {
		if b != 0 {
			return false
		}
	}
	return true
}

func newTokenID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", platform.Wrap(platform.CodeInternal, err, "generate capability token id")
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func readKeyFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, fmt.Sprintf("read key file %s", path))
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, fmt.Sprintf("decode key file %s", path))
	}
	return decoded, nil
}

func writeKeyFile(path string, data []byte) error {
	encoded := base64.RawURLEncoding.EncodeToString(data) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return platform.Wrap(platform.CodeIO, err, fmt.Sprintf("write key file %s", path))
	}
	return nil
}

func appendUint16(dst []byte, value uint16) []byte {
	return append(dst, byte(value>>8), byte(value))
}

func appendInt64(dst []byte, value int64) []byte {
	u := uint64(value)
	return append(dst,
		byte(u>>56), byte(u>>48), byte(u>>40), byte(u>>32),
		byte(u>>24), byte(u>>16), byte(u>>8), byte(u),
	)
}

func appendString(dst []byte, value string) []byte {
	dst = appendUint16(dst, uint16(len(value)))
	return append(dst, value...)
}

func appendStringList(dst []byte, values []string) []byte {
	dst = appendUint16(dst, uint16(len(values)))
	for _, value := range values {
		dst = appendString(dst, value)
	}
	return dst
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

func (d *decoder) readUint16() (uint16, error) {
	if d.off+2 > len(d.data) {
		return 0, platform.New(platform.CodeIO, "unexpected end of input")
	}
	value := uint16(d.data[d.off])<<8 | uint16(d.data[d.off+1])
	d.off += 2
	return value, nil
}

func (d *decoder) readInt64() (int64, error) {
	if d.off+8 > len(d.data) {
		return 0, platform.New(platform.CodeIO, "unexpected end of input")
	}
	u := uint64(d.data[d.off])<<56 | uint64(d.data[d.off+1])<<48 | uint64(d.data[d.off+2])<<40 | uint64(d.data[d.off+3])<<32 |
		uint64(d.data[d.off+4])<<24 | uint64(d.data[d.off+5])<<16 | uint64(d.data[d.off+6])<<8 | uint64(d.data[d.off+7])
	d.off += 8
	return int64(u), nil
}

func (d *decoder) readString() (string, error) {
	length, err := d.readUint16()
	if err != nil {
		return "", err
	}
	bytes, err := d.readFixed(int(length))
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (d *decoder) readStringList() ([]string, error) {
	count, err := d.readUint16()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read capability string list count")
	}
	out := make([]string, count)
	for i := 0; i < int(count); i++ {
		value, err := d.readString()
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read capability string list value")
		}
		out[i] = value
	}
	return out, nil
}

func (d *decoder) remaining() int {
	return len(d.data) - d.off
}
