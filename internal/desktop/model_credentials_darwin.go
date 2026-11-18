//go:build darwin && cgo

package desktop

import (
	"errors"

	"github.com/keybase/go-keychain"
)

type systemModelCredentialStore struct{}

func modelKeychainItem(account string) keychain.Item {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(modelCredentialService)
	item.SetAccount(account)
	item.SetSynchronizable(keychain.SynchronizableNo)
	return item
}

func (systemModelCredentialStore) Get(account string) (string, error) {
	query := modelKeychainItem(account)
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)
	found, err := keychain.QueryItem(query)
	if errors.Is(err, keychain.ErrorItemNotFound) || (err == nil && len(found) == 0) {
		return "", errModelCredentialNotFound
	}
	if err != nil {
		return "", err
	}
	return string(found[0].Data), nil
}

func (systemModelCredentialStore) Set(account, value string) error {
	item := modelKeychainItem(account)
	item.SetLabel(modelCredentialService)
	item.SetData([]byte(value))
	err := keychain.AddItem(item)
	if !errors.Is(err, keychain.ErrorDuplicateItem) {
		return err
	}
	update := keychain.NewItem()
	update.SetData([]byte(value))
	return keychain.UpdateItem(modelKeychainItem(account), update)
}
