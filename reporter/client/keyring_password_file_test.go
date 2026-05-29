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
