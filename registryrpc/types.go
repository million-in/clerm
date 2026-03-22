package registryrpc

import (
	"net/http"
	"time"

	"github.com/million-in/clerm/schema"
)

type MethodSummary struct {
	Reference    string `json:"reference"`
	Relation     string `json:"relation"`
	Condition    string `json:"condition"`
	Execution    string `json:"execution"`
	InputCount   int    `json:"input_count"`
	OutputCount  int    `json:"output_count"`
	OutputFormat string `json:"output_format"`
}

type RelationSummary struct {
	Name          string `json:"name"`
	Condition     string `json:"condition"`
	Status        string `json:"status,omitempty"`
	TokenRequired bool   `json:"token_required"`
}

type SchemaSummary struct {
	Fingerprint       string            `json:"fingerprint"`
	PublicFingerprint string            `json:"public_fingerprint"`
	SchemaName        string            `json:"schema_name"`
	OwnerID           string            `json:"owner_id"`
	Status            string            `json:"status"`
	Metadata          schema.Metadata   `json:"metadata,omitempty"`
	Methods           []MethodSummary   `json:"methods"`
	Relations         []RelationSummary `json:"relations"`
}

type Relationship struct {
	ConsumerID          string    `json:"consumer_id"`
	ProviderFingerprint string    `json:"provider_fingerprint"`
	Relation            string    `json:"relation"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type RegisterInput struct {
	OwnerID string
	Status  string
	Payload []byte
}

type RegisterOutput struct {
	Schema SchemaSummary `json:"schema"`
}

type SearchInput struct {
	ConsumerID string   `json:"consumer_id"`
	Query      string   `json:"query"`
	Relations  []string `json:"relations,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
}

type SearchOutput struct {
	Results []SchemaSummary `json:"results"`
}

type RelationshipInput struct {
	ConsumerID          string `json:"consumer_id"`
	ProviderFingerprint string `json:"provider_fingerprint"`
	Relation            string `json:"relation"`
	Status              string `json:"status,omitempty"`
}

type RelationshipOutput struct {
	Relationship Relationship `json:"relationship"`
}

type RelationshipStatusInput struct {
	ConsumerID          string `json:"consumer_id"`
	ProviderFingerprint string `json:"provider_fingerprint"`
}

type RelationshipStatusOutput struct {
	Relationships []Relationship `json:"relationships"`
}

type IssueTokenInput struct {
	ConsumerID          string   `json:"consumer_id"`
	ProviderFingerprint string   `json:"provider_fingerprint"`
	Method              string   `json:"method,omitempty"`
	Relation            string   `json:"relation,omitempty"`
	Subject             string   `json:"subject,omitempty"`
	Targets             []string `json:"targets,omitempty"`
	InvokeTTLSeconds    int64    `json:"invoke_ttl_seconds,omitempty"`
	RefreshTTLSeconds   int64    `json:"refresh_ttl_seconds,omitempty"`
}

type IssueTokenOutput struct {
	CapabilityToken string `json:"capability_token"`
	ExpiresAt       string `json:"expires_at"`
	RefreshToken    string `json:"refresh_token"`
	RefreshExpires  string `json:"refresh_expires_at"`
	Relation        string `json:"relation"`
	Condition       string `json:"condition"`
}

type RefreshTokenInput struct {
	RefreshToken      string   `json:"refresh_token"`
	Targets           []string `json:"targets,omitempty"`
	InvokeTTLSeconds  int64    `json:"invoke_ttl_seconds,omitempty"`
	RefreshTTLSeconds int64    `json:"refresh_ttl_seconds,omitempty"`
}

type InvokeInput struct {
	ProviderFingerprint string
	Payload             []byte
}

type InvokeOutput struct {
	StatusCode    int         `json:"status_code"`
	Headers       http.Header `json:"headers,omitempty"`
	Body          []byte      `json:"-"`
	Target        string      `json:"target,omitempty"`
	CommandMethod string      `json:"command_method,omitempty"`
}
