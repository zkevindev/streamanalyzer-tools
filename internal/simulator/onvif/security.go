package onvif

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

const (
	digestType  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest"
	tokenWindow = 5 * time.Minute
)

type soapSecurity struct {
	Token *usernameToken `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd UsernameToken"`
}

type usernameToken struct {
	Username string `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd Username"`
	Password struct {
		Type   string `xml:"Type,attr"`
		Digest string `xml:",chardata"`
	} `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd Password"`
	Nonce   string `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd Nonce"`
	Created string `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd Created"`
}

type soapAuth struct {
	username, password string
	mu                 sync.Mutex
	nonces             map[string]time.Time
}

func (c *camera) clock() time.Time {
	return c.deviceTime(time.Now())
}

func (a *soapAuth) verify(token *usernameToken, now time.Time) (bool, bool) {
	if token == nil || token.Username != a.username || token.Password.Type != digestType {
		return false, false
	}
	nonce, nonceErr := base64.StdEncoding.DecodeString(strings.TrimSpace(token.Nonce))
	given, digestErr := base64.StdEncoding.DecodeString(strings.TrimSpace(token.Password.Digest))
	if nonceErr != nil || len(nonce) == 0 || len(nonce) > 128 || digestErr != nil || len(given) != sha1.Size {
		return false, false
	}
	created, err := time.Parse(time.RFC3339Nano, token.Created)
	if err != nil {
		return false, false
	}
	hash := sha1.New()
	hash.Write(nonce)
	hash.Write([]byte(token.Created))
	hash.Write([]byte(a.password))
	if subtle.ConstantTimeCompare(hash.Sum(nil), given) != 1 {
		return false, false
	}
	if created.Before(now.Add(-tokenWindow)) || created.After(now.Add(tokenWindow)) {
		return false, true
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for key, expiry := range a.nonces {
		if !now.Before(expiry) {
			delete(a.nonces, key)
		}
	}
	key := string(nonce)
	if _, seen := a.nonces[key]; seen || len(a.nonces) >= 2048 {
		return false, false
	}
	a.nonces[key] = created.Add(tokenWindow)
	return true, false
}
