// Package secrets is the thin wrapper around the OS keychain used by
// hackermode. Secrets are partitioned by module ID so a malicious module
// cannot read another module's secrets — the keyring service name is
// "hackermode/<module-id>" and the account is the secret's key.
package secrets

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned when a secret is not present in the keychain.
var ErrNotFound = errors.New("secrets: not found")

func service(moduleID string) string { return "hackermode/" + moduleID }

// Get returns the secret value for a (module, key) pair.
func Get(moduleID, key string) (string, error) {
	if moduleID == "" || key == "" {
		return "", errors.New("secrets: module and key required")
	}
	v, err := keyring.Get(service(moduleID), key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("keyring get: %w", err)
	}
	return v, nil
}

// Set stores a secret value. Any existing value is overwritten.
func Set(moduleID, key, value string) error {
	if moduleID == "" || key == "" {
		return errors.New("secrets: module and key required")
	}
	return keyring.Set(service(moduleID), key, value)
}

// Delete removes a secret. Missing keys are treated as success.
func Delete(moduleID, key string) error {
	err := keyring.Delete(service(moduleID), key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
