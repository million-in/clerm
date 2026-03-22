package capability

import (
	"crypto/ed25519"
	"time"

	internal "github.com/million-in/clerm/internal/capability"
)

type Token = internal.Token
type InspectView = internal.InspectView
type IssueOptions = internal.IssueOptions
type Keyring = internal.Keyring
type ReplayStore = internal.ReplayStore
type MemoryReplayStore = internal.MemoryReplayStore

var (
	NewKeyring = func(keys map[string]ed25519.PublicKey) *Keyring { return internal.NewKeyring(keys) }
	Issue      = func(opts IssueOptions, privateKey ed25519.PrivateKey) (*Token, error) {
		return internal.Issue(opts, privateKey)
	}
	Sign             = internal.Sign
	Validate         = internal.Validate
	ValidateUnsigned = internal.ValidateUnsigned
	VerifyTime       = func(token *Token, now time.Time, skew time.Duration) error {
		return internal.VerifyTime(token, now, skew)
	}
	EncodeText           = internal.EncodeText
	DecodeText           = internal.DecodeText
	Encode               = internal.Encode
	Decode               = internal.Decode
	ReadPrivateKeyFile   = internal.ReadPrivateKeyFile
	ReadPublicKeyFile    = internal.ReadPublicKeyFile
	WritePrivateKeyFile  = internal.WritePrivateKeyFile
	WritePublicKeyFile   = internal.WritePublicKeyFile
	GenerateKeyPair      = internal.GenerateKeyPair
	NewMemoryReplayStore = func() *MemoryReplayStore { return internal.NewMemoryReplayStore() }
)
