package main

import (
	"context"
	"crypto/ed25519"
	"log"
	"net/http"
	"os"

	"github.com/million-in/clerm/capability"
	"github.com/million-in/clerm/resolver"
)

func main() {
	schemaPath := getenv("CLERM_SCHEMA", "schemas/provider_search.clermcfg")
	listen := getenv("LISTEN_ADDR", "127.0.0.1:8282")
	publicKeyPath := os.Getenv("CLERM_PUBLIC_KEY")

	service, err := resolver.LoadConfig(schemaPath)
	if err != nil {
		log.Fatal(err)
	}
	if publicKeyPath != "" {
		publicKey, err := capability.ReadPublicKeyFile(publicKeyPath)
		if err != nil {
			log.Fatal(err)
		}
		service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{"registry": publicKey}))
	}
	must(service.Bind("@global.healthcare.search_providers.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.Success(map[string]any{
			"request_id": "123e4567-e89b-12d3-a456-426614174000",
			"providers": []map[string]any{{"id": "provider-1", "name": "Cardio Clinic"}},
		}), nil
	}))
	must(service.Bind("@verified.healthcare.book_visit.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.Success(map[string]any{
			"order_id": "visit-001",
			"status":   "confirmed",
		}), nil
	}))

	mux := http.NewServeMux()
	mux.Handle("/api", service.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})))

	log.Printf("go sample listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, mux))
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
