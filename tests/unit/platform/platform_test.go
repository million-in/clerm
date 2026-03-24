package platform_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/million-in/clerm/platform"
)

func TestErrorHelpersRoundTripCodes(t *testing.T) {
	base := errors.New("disk failed")
	err := platform.Wrap(platform.CodeIO, base, "read file")
	if !platform.IsCode(err, platform.CodeIO) {
		t.Fatalf("expected io code, got %v", err)
	}
	if platform.CodeOf(err) != platform.CodeIO {
		t.Fatalf("unexpected code: %s", platform.CodeOf(err))
	}
	coded := platform.As(err)
	if coded == nil || coded.Message != "read file" {
		t.Fatalf("unexpected coded error: %#v", coded)
	}
}

func TestNewLoggerRejectsInvalidLevel(t *testing.T) {
	if _, err := platform.NewLogger("verbose"); err == nil || !platform.IsCode(err, platform.CodeInvalidArgument) {
		t.Fatalf("expected invalid level error, got %v", err)
	}
}

func TestLogErrorIncludesCodeFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	err := platform.New(platform.CodeNotFound, "missing schema")
	platform.LogError(logger, "lookup failed", err, "target", "registry")
	output := buf.String()
	for _, expected := range []string{"lookup failed", "code=not_found", "not_found=true", "target=registry"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in log output %q", expected, output)
		}
	}
}
