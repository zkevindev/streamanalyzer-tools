package hlsorigin

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKeyRotationAndForbiddenKey(t *testing.T) {
	for _, mode := range []string{"key_rotation", "key_forbidden"} {
		t.Run(mode, func(t *testing.T) {
			var m Manager
			t.Cleanup(m.Close)
			status, err := m.Start(Config{Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			m.live = liveClock{last: keyRotationSequence, frozen: true}
			get := func(name string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				m.Serve(w, httptest.NewRequest(http.MethodGet, "/"+name, nil), status.ID, name)
				return w
			}
			playlist := get("playlist.m3u8")
			if text := playlist.Body.String(); playlist.Code != 200 ||
				!strings.Contains(text, "URI=\"key-1.bin\"\n#EXTINF:2.000,\nlive-4.ts") ||
				!strings.Contains(text, "URI=\"key-2.bin\"\n#EXTINF:2.000,\nlive-6.ts") {
				t.Fatal(playlist.Code, text)
			}
			oldKey := get("key-1.bin")
			rotated := get("key-2.bin")
			if mode == "key_forbidden" {
				if rotated.Code != 403 || m.Status().KeyRotation.K2Denied != 1 {
					t.Fatal("new key should be denied", rotated.Code)
				}
				if segment := get("live-6.ts"); segment.Code != 200 || segment.Body.Len() == 0 {
					t.Fatal("encrypted media unavailable during key denial", segment.Code)
				}
				if _, err := m.RestoreKey("old-id"); err == nil {
					t.Fatal("restored another source")
				}
				restored, err := m.RestoreKey(status.ID)
				if err != nil || restored.KeyRotation.RestoredAt.Before(time.Now().Add(-time.Second)) {
					t.Fatalf("restore failed: %+v %v", restored, err)
				}
				rotated = get("key-2.bin")
			}
			if oldKey.Code != 200 || rotated.Code != 200 || bytes.Equal(oldKey.Body.Bytes(), rotated.Body.Bytes()) {
				t.Fatal("expected two distinct accessible keys")
			}
			for _, item := range []struct {
				key []byte
				seq uint64
			}{{oldKey.Body.Bytes(), 5}, {rotated.Body.Bytes(), 6}} {
				num := strconv.FormatUint(item.seq, 10)
				encrypted := get("live-" + num + ".ts")
				actual := encrypted.Body.Bytes()
				want, err := media.ReadFile("media/segment-0" + num + ".ts")
				if err != nil || encrypted.Code != 200 || len(actual)%aes.BlockSize != 0 || bytes.HasPrefix(actual, want) {
					t.Fatal("expected encrypted segment", encrypted.Code, err)
				}
				block, err := aes.NewCipher(item.key)
				if err != nil {
					t.Fatal(err)
				}
				var iv [aes.BlockSize]byte
				binary.BigEndian.PutUint64(iv[8:], item.seq)
				cipher.NewCBCDecrypter(block, iv[:]).CryptBlocks(actual, actual)
				pad := int(actual[len(actual)-1])
				if pad < 1 || pad > aes.BlockSize || !bytes.Equal(actual[:len(actual)-pad], want) {
					t.Fatal("segment did not decrypt with playlist key and media sequence", num)
				}
			}
		})
	}
}
