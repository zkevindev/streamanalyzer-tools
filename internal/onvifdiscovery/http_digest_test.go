package onvifdiscovery

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/icholy/digest"
)

func TestHTTPDigestOneChallengeWithFreshUsernameToken(t *testing.T) {
	for _, algorithm := range []string{"MD5", "SHA-256"} {
		for _, qop := range []string{"auth", "auth-int"} {
			t.Run(algorithm+"/"+qop, func(t *testing.T) {
				const username = "digest-camera"
				const password = "digest-private-password"
				var count atomic.Int64
				var firstBody string
				hash := func(value string) string {
					if algorithm == "MD5" {
						v := md5.Sum([]byte(value))
						return hex.EncodeToString(v[:])
					}
					v := sha256.Sum256([]byte(value))
					return hex.EncodeToString(v[:])
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					if strings.Contains(string(body), password) {
						t.Error("plaintext password appeared in SOAP request")
					}
					if count.Add(1) == 1 {
						firstBody = string(body)
						if r.Header.Get("Authorization") != "" {
							t.Error("credentials sent before Digest challenge")
						}
						w.Header().Add("WWW-Authenticate", `Basic realm="unused"`)
						w.Header().Add("WWW-Authenticate", fmt.Sprintf(`Digest realm="camera", nonce="bounded-nonce", algorithm=%s, qop="%s"`, algorithm, qop))
						w.WriteHeader(401)
						return
					}
					if string(body) == firstBody {
						t.Error("HTTP Digest retry reused the WSSE nonce/message")
					}
					credentials, err := digest.ParseCredentials(r.Header.Get("Authorization"))
					if err != nil {
						t.Error(err)
						w.WriteHeader(401)
						return
					}
					if credentials.Username != username || credentials.URI != r.URL.RequestURI() || credentials.Nc != 1 || credentials.Algorithm != algorithm {
						t.Errorf("incorrect HTTP digest identity: %+v", credentials)
					}
					ha2 := hash("POST:" + r.URL.RequestURI())
					if qop == "auth-int" {
						ha2 = hash("POST:" + r.URL.RequestURI() + ":" + hash(string(body)))
					}
					want := hash(hash(username+":camera:"+password) + ":bounded-nonce:00000001:" + credentials.Cnonce + ":" + qop + ":" + ha2)
					if credentials.Response != want {
						t.Error("incorrect independent Digest response hash")
					}
					io.WriteString(w, reply("GetCapabilities", ""))
				}))
				defer server.Close()
				nic, _ := bindInterface("127.0.0.1")
				destination, err := resolveTarget(t.Context(), server.URL+"/onvif/device?channel=1&mode=test", nic, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := call(t.Context(), destination, deviceNS, "GetCapabilities", "", Credentials{Username: username, Password: password}, 0, nil); err != nil {
					t.Fatal(err)
				}
				if count.Load() != 2 {
					t.Fatalf("expected one challenge/retry, got %d requests", count.Load())
				}
			})
		}
	}
}

func TestHTTPDigestBoundAndNoBasicOrRedirect(t *testing.T) {
	for _, kind := range []string{"always401", "forbidden403", "basic_only", "digest_redirect"} {
		t.Run(kind, func(t *testing.T) {
			var count, trapCalls atomic.Int64
			trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { trapCalls.Add(1); w.WriteHeader(200) }))
			defer trap.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := count.Add(1)
				if kind == "forbidden403" {
					w.WriteHeader(403)
					return
				}
				if kind == "digest_redirect" && attempt == 2 {
					w.Header().Set("Location", trap.URL)
					w.WriteHeader(302)
					return
				}
				if kind == "basic_only" {
					w.Header().Set("WWW-Authenticate", `Basic realm="camera"`)
				} else {
					w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="one-retry", algorithm=MD5, qop="auth"`)
				}
				w.WriteHeader(401)
			}))
			defer server.Close()
			nic, _ := bindInterface("127.0.0.1")
			destination, err := resolveTarget(t.Context(), server.URL, nic, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = call(t.Context(), destination, deviceNS, "GetCapabilities", "", Credentials{Username: "private-user", Password: "private-password"}, 0, nil)
			want := "auth_failed"
			if kind == "digest_redirect" {
				want = "redirect_stopped"
			}
			if errorCode(err) != want {
				t.Fatalf("expected %s, got %v", want, err)
			}
			requests := int64(2)
			if kind == "forbidden403" || kind == "basic_only" {
				requests = 1
			}
			if count.Load() != requests || trapCalls.Load() != 0 {
				t.Fatalf("authentication exceeded its bound or redirected: requests=%d trap=%d", count.Load(), trapCalls.Load())
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("credentials leaked in Digest error")
			}
		})
	}
}
