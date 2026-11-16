// Package secrets stores source credentials. It prefers the operating system
// keyring and falls back to an encrypted 0600 file when no keyring is available.
//
// The fallback keeps a random key next to the encrypted values so a copied data
// directory remains unusable without it. It is not a replacement for a real
// keyring, which is why the keyring is tried first.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned when no secret is stored for a reference.
var ErrNotFound = errors.New("secret not found")

// Store keeps credentials out of the SQLite database.
type Store interface {
	Set(ref, value string) error
	Get(ref string) (string, error)
	Delete(ref string) error
}

const (
	keyringService = "Substrackt"
	keyFile        = "secrets.key"
	dataFile       = "secrets.enc"
)

// Open returns a store that uses the OS keyring when it is reachable and an
// encrypted file under dir otherwise.
func Open(dir string) (Store, error) {
	file, err := newFileStore(dir)
	if err != nil {
		return nil, err
	}
	return &fallbackStore{file: file}, nil
}

// NewFileStore returns the encrypted-file backend directly. It is useful for
// tests and for hosts where the keyring is known to be unavailable.
func NewFileStore(dir string) (Store, error) {
	return newFileStore(dir)
}

// NewMemoryStore returns a store that keeps secrets in memory. It is intended
// for tests.
func NewMemoryStore() Store { return &memoryStore{values: make(map[string]string)} }

type fallbackStore struct {
	file *fileStore
}

func (s *fallbackStore) Set(ref, value string) error {
	if err := keyring.Set(keyringService, ref, value); err == nil {
		return nil
	}
	return s.file.Set(ref, value)
}

func (s *fallbackStore) Get(ref string) (string, error) {
	value, err := keyring.Get(keyringService, ref)
	if err == nil {
		return value, nil
	}
	value, fileErr := s.file.Get(ref)
	if fileErr == nil {
		return value, nil
	}
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(fileErr, ErrNotFound) {
		return "", ErrNotFound
	}
	return "", fileErr
}

func (s *fallbackStore) Delete(ref string) error {
	keyringErr := keyring.Delete(keyringService, ref)
	if errors.Is(keyringErr, keyring.ErrNotFound) {
		keyringErr = nil
	}
	fileErr := s.file.Delete(ref)
	if errors.Is(fileErr, ErrNotFound) {
		fileErr = nil
	}
	if fileErr != nil {
		return fileErr
	}
	if keyringErr == nil {
		return nil
	}
	// The keyring may simply be unavailable on this host. Only surface the
	// delete error when the secret is still readable from it.
	if _, err := keyring.Get(keyringService, ref); err == nil {
		return keyringErr
	}
	return nil
}

type fileStore struct {
	dir string
	mu  sync.Mutex
}

func newFileStore(dir string) (*fileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("protect secrets directory: %w", err)
	}
	return &fileStore{dir: dir}, nil
}

func (s *fileStore) Set(ref, value string) error {
	if ref == "" {
		return fmt.Errorf("secret reference is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.load()
	if err != nil {
		return err
	}
	aead, err := s.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), []byte(ref))
	values[ref] = base64.StdEncoding.EncodeToString(sealed)
	return s.save(values)
}

func (s *fileStore) Get(ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.load()
	if err != nil {
		return "", err
	}
	encoded, ok := values[ref]
	if !ok {
		return "", ErrNotFound
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	aead, err := s.aead()
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", fmt.Errorf("secret %s is corrupt", ref)
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, []byte(ref))
	if err != nil {
		return "", fmt.Errorf("decrypt secret %s: %w", ref, err)
	}
	return string(plain), nil
}

func (s *fileStore) Delete(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := values[ref]; !ok {
		return ErrNotFound
	}
	delete(values, ref)
	return s.save(values)
}

func (s *fileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, dataFile))
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read secrets file: %w", err)
	}
	values := make(map[string]string)
	if len(data) == 0 {
		return values, nil
	}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("decode secrets file: %w", err)
	}
	return values, nil
}

func (s *fileStore) save(values map[string]string) error {
	data, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode secrets file: %w", err)
	}
	temp, err := os.CreateTemp(s.dir, ".secrets-*")
	if err != nil {
		return fmt.Errorf("create secrets temp file: %w", err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("protect secrets temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write secrets file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("flush secrets file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close secrets file: %w", err)
	}
	if err := os.Rename(tempName, filepath.Join(s.dir, dataFile)); err != nil {
		return fmt.Errorf("replace secrets file: %w", err)
	}
	return nil
}

func (s *fileStore) aead() (cipher.AEAD, error) {
	key, err := s.loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create secret AEAD: %w", err)
	}
	return aead, nil
}

func (s *fileStore) loadKey() ([]byte, error) {
	path := filepath.Join(s.dir, keyFile)
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("secret key has invalid length %d", len(key))
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read secret key: %w", err)
	}

	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	// O_EXCL prevents two concurrent first runs from replacing each other's key.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create secret key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write secret key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close secret key: %w", err)
	}
	return key, nil
}

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *memoryStore) Set(ref, value string) error {
	if ref == "" {
		return fmt.Errorf("secret reference is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[ref] = value
	return nil
}

func (s *memoryStore) Get(ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[ref]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (s *memoryStore) Delete(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[ref]; !ok {
		return ErrNotFound
	}
	delete(s.values, ref)
	return nil
}
