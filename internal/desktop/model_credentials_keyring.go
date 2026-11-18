//go:build linux || windows

package desktop

import (
	"errors"

	"github.com/zalando/go-keyring"
)

type systemModelCredentialStore struct{}

func (systemModelCredentialStore) Get(account string) (string, error) {
	value, err := keyring.Get(modelCredentialService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errModelCredentialNotFound
	}
	return value, err
}

func (systemModelCredentialStore) Set(account, value string) error {
	err := keyring.Set(modelCredentialService, account, value)
	if errors.Is(err, keyring.ErrSetDataTooBig) {
		return errModelCredentialTooLarge
	}
	return err
}
