//go:build (!darwin || !cgo) && !linux && !windows

package desktop

type systemModelCredentialStore struct{}

func (systemModelCredentialStore) Get(string) (string, error) {
	return "", errModelCredentialsUnavailable
}

func (systemModelCredentialStore) Set(string, string) error {
	return errModelCredentialsUnavailable
}
