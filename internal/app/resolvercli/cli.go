package resolvercli

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/million-in/clerm"
	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/resolver"
)

type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
}

func Run(logger *slog.Logger, args []string) error {
	return RunWithIO(logger, Streams{Stdout: os.Stdout, Stderr: os.Stderr}, args)
}

func RunWithIO(logger *slog.Logger, streams Streams, args []string) error {
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}
	fs := flag.NewFlagSet("clerm-resolver", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermcfg")
	schemaURL := fs.String("schema-url", "", "URL to .clermcfg")
	listen := fs.String("listen", "127.0.0.1:8181", "listen address")
	unixSocket := fs.String("unix-socket", "", "Unix domain socket path")
	publicKeyPath := fs.String("cap-public-key", "", "path to Ed25519 public key for capability verification")
	keyID := fs.String("cap-key-id", "registry", "capability verification key id")
	maxBodyBytes := fs.Int64("max-body-bytes", 1<<20, "maximum CLERM payload size in bytes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*schemaPath) == "" && strings.TrimSpace(*schemaURL) == "" {
		return platform.New(platform.CodeInvalidArgument, "clerm-resolver requires -schema or -schema-url")
	}
	if strings.TrimSpace(*schemaPath) != "" && strings.TrimSpace(*schemaURL) != "" {
		return platform.New(platform.CodeInvalidArgument, "use either -schema or -schema-url, not both")
	}
	service, err := loadService(*schemaPath, *schemaURL, *publicKeyPath, *keyID)
	if err != nil {
		return err
	}
	service.SetMaxBodyBytes(*maxBodyBytes)
	server := &http.Server{
		Addr:              *listen,
		Handler:           clerm.Resolver.NewDaemonHandler(logger, service),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	listener, address, err := daemonListener(*listen, *unixSocket)
	if err != nil {
		return err
	}
	if logger != nil {
		logger.Info("starting clerm resolver daemon",
			"listen", address,
			"schema", service.Document().Name,
			"schema_fingerprint", fmt.Sprintf("%x", service.Fingerprint()),
		)
	}
	_, _ = fmt.Fprintf(streams.Stdout, "%s\n", address)
	return server.Serve(listener)
}

func loadService(schemaPath string, schemaURL string, publicKeyPath string, keyID string) (*resolver.Service, error) {
	options := clerm.ServiceOptions{}
	if strings.TrimSpace(publicKeyPath) != "" {
		publicKey, err := capability.ReadPublicKeyFile(strings.TrimSpace(publicKeyPath))
		if err != nil {
			return nil, err
		}
		options.CapabilityKeyring = capability.NewKeyring(map[string]ed25519.PublicKey{strings.TrimSpace(keyID): publicKey})
	}
	switch {
	case strings.TrimSpace(schemaPath) != "":
		return clerm.Resolver.LoadService(strings.TrimSpace(schemaPath), options)
	case strings.TrimSpace(schemaURL) != "":
		return clerm.Resolver.LoadServiceURL(context.Background(), strings.TrimSpace(schemaURL), clerm.RemoteServiceOptions{
			Load: resolver.LoadConfigURLOptions{
				URLPolicy: resolver.DenyPrivateHostPolicy,
			},
			Service: options,
		})
	default:
		return nil, platform.New(platform.CodeInvalidArgument, "schema source is required")
	}
}

func daemonListener(listen string, unixSocket string) (net.Listener, string, error) {
	if strings.TrimSpace(unixSocket) == "" {
		listener, err := net.Listen("tcp", strings.TrimSpace(listen))
		if err != nil {
			return nil, "", platform.Wrap(platform.CodeIO, err, "listen on TCP address")
		}
		return listener, listener.Addr().String(), nil
	}
	path := filepath.Clean(strings.TrimSpace(unixSocket))
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", platform.Wrap(platform.CodeIO, err, "remove existing Unix socket")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", platform.Wrap(platform.CodeIO, err, "listen on Unix socket")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, "", platform.Wrap(platform.CodeIO, err, "set Unix socket permissions")
	}
	return listener, path, nil
}
