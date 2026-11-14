// Package store provides Substrackt's embedded local database, backed by bbolt.
//
// The database lives in the user's config directory and is safe for concurrent
// use: bbolt allows many readers plus a single writer per process.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"
)

// Buckets used by the application. Add new buckets here so they are created on
// open instead of being created ad hoc by callers.
var (
	BucketAccounts = []byte("accounts")
	BucketSettings = []byte("settings")
)

// Store wraps an open bbolt database.
type Store struct {
	db *bbolt.DB
}

// DefaultPath returns <user config dir>/Substrackt/substrackt.db.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, "Substrackt", "substrackt.db"), nil
}

// OpenDefault opens (creating if needed) the store at DefaultPath.
func OpenDefault() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	return Open(path)
}

// Open opens the bbolt database at path, creating the file and the application
// buckets if they do not exist yet.
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}

	s := &Store{db: db}
	if err := s.createBuckets(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) createBuckets() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{BucketAccounts, BucketSettings} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q: %w", name, err)
			}
		}
		return nil
	})
}

// Close closes the database. It is safe to call on a nil store.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Get returns a copy of the value stored under key in bucket, or nil if the key
// does not exist.
func (s *Store) Get(bucket, key []byte) ([]byte, error) {
	var value []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return fmt.Errorf("bucket %q does not exist", bucket)
		}
		if raw := b.Get(key); raw != nil {
			// bbolt values are only valid for the duration of the transaction.
			value = append([]byte(nil), raw...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get %q from %q: %w", key, bucket, err)
	}
	return value, nil
}

// Put stores value under key in bucket, creating the bucket if necessary.
func (s *Store) Put(bucket, key, value []byte) error {
	if err := s.db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}
		return b.Put(key, value)
	}); err != nil {
		return fmt.Errorf("put %q into %q: %w", key, bucket, err)
	}
	return nil
}

// Delete removes key from bucket. Deleting a missing key or bucket is not an
// error.
func (s *Store) Delete(bucket, key []byte) error {
	if err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		return b.Delete(key)
	}); err != nil {
		return fmt.Errorf("delete %q from %q: %w", key, bucket, err)
	}
	return nil
}
