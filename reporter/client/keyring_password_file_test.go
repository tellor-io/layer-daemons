package client

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyringReaderPasswordFile(t *testing.T) {
	t.Run("missing file returns keyring password file error", func(t *testing.T) {
		t.Setenv("KEYRING_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing-password"))

		reader, usingPasswordFile, err := keyringReader()

		if reader != nil {
			t.Fatalf("expected no reader, got %T", reader)
		}
		if !usingPasswordFile {
			t.Fatalf("expected KEYRING_PASSWORD_FILE to be detected")
		}
		if !errors.Is(err, ErrKeyringPasswordFile) {
			t.Fatalf("expected ErrKeyringPasswordFile, got %v", err)
		}
	})

	t.Run("empty file returns keyring password file error", func(t *testing.T) {
		passFile := filepath.Join(t.TempDir(), "password")
		if err := os.WriteFile(passFile, []byte("\n"), 0o600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}
		t.Setenv("KEYRING_PASSWORD_FILE", passFile)

		reader, usingPasswordFile, err := keyringReader()

		if reader != nil {
			t.Fatalf("expected no reader, got %T", reader)
		}
		if !usingPasswordFile {
			t.Fatalf("expected KEYRING_PASSWORD_FILE to be detected")
		}
		if !errors.Is(err, ErrKeyringPasswordFile) {
			t.Fatalf("expected ErrKeyringPasswordFile, got %v", err)
		}
	})

	t.Run("valid file returns repeated password reader", func(t *testing.T) {
		passFile := filepath.Join(t.TempDir(), "password")
		if err := os.WriteFile(passFile, []byte("secret\n"), 0o600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}
		t.Setenv("KEYRING_PASSWORD_FILE", passFile)

		reader, usingPasswordFile, err := keyringReader()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !usingPasswordFile {
			t.Fatalf("expected KEYRING_PASSWORD_FILE to be detected")
		}
		data, err := io.ReadAll(io.LimitReader(reader, int64(len("secret\n")*128)))
		if err != nil {
			t.Fatalf("failed to read password reader: %v", err)
		}
		if got := strings.Count(string(data), "secret\n"); got != 128 {
			t.Fatalf("expected password repeated 128 times, got %d", got)
		}
	})
}

func TestValidateKeyringBackendConfig(t *testing.T) {
	t.Run("accepts supported backend", func(t *testing.T) {
		if err := validateKeyringBackendConfig("file", false); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("rejects empty backend", func(t *testing.T) {
		err := validateKeyringBackendConfig("", false)
		if err == nil || !strings.Contains(err.Error(), "keyring-backend is required") {
			t.Fatalf("expected required backend error, got %v", err)
		}
	})

	t.Run("rejects unsupported backend", func(t *testing.T) {
		err := validateKeyringBackendConfig("not-real", false)
		if err == nil || !strings.Contains(err.Error(), "unsupported keyring-backend") {
			t.Fatalf("expected unsupported backend error, got %v", err)
		}
	})

	t.Run("password file requires file backend", func(t *testing.T) {
		err := validateKeyringBackendConfig("test", true)
		if !errors.Is(err, ErrKeyringPasswordFile) {
			t.Fatalf("expected ErrKeyringPasswordFile, got %v", err)
		}
		if !strings.Contains(err.Error(), "KEYRING_BACKEND=file") {
			t.Fatalf("expected KEYRING_BACKEND=file hint, got %v", err)
		}
	})
}
