package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreRoundTripAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err := store.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
	if err := store.Set("", "value"); err == nil {
		t.Fatal("expected an empty reference to be rejected")
	}
	if err := store.Set("source:one", "hunter2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := store.Get("source:one")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("value = %q", got)
	}

	info, err := os.Stat(filepath.Join(dir, dataFile))
	if err != nil {
		t.Fatalf("stat data: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("data mode = %o, want 600", info.Mode().Perm())
	}
	keyInfo, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600", keyInfo.Mode().Perm())
	}
	raw, err := os.ReadFile(filepath.Join(dir, dataFile))
	if err != nil {
		t.Fatalf("read data: %v", err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("credentials were stored in plaintext")
	}

	if err := store.Delete("source:one"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get("source:one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete error = %v", err)
	}
	if err := store.Delete("source:one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete error = %v", err)
	}
}

func TestFileStoreRejectsWrongKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Set("source:one", "secret-value"); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Replace the key with an unrelated one: the ciphertext must not open.
	wrong := make([]byte, 32)
	for i := range wrong {
		wrong[i] = byte(i + 1)
	}
	if err := os.WriteFile(filepath.Join(dir, keyFile), wrong, 0o600); err != nil {
		t.Fatalf("write wrong key: %v", err)
	}
	if _, err := store.Get("source:one"); err == nil {
		t.Fatal("decrypting with the wrong key should fail")
	}
}

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Set("source:one", "value"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := store.Get("source:one")
	if err != nil || got != "value" {
		t.Fatalf("get = %q, %v", got, err)
	}
	if err := store.Delete("source:one"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get("source:one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete error = %v", err)
	}
}
