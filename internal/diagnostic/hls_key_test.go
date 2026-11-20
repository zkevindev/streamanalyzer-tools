package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"streamanalyzer/internal/hlsdelivery/m3u8"
)

func hlsTestEncrypt(t *testing.T, plaintext, key, iv []byte) []byte {
	t.Helper()
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	plaintext = append(plaintext, bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(result, plaintext)
	return result
}

func TestHLSContentAnalysisUsesEphemeralAESKey(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := make([]byte, 16)
	iv[15] = 7
	plaintext := bytes.Repeat([]byte{0x47, 1, 2, 3, 4, 5, 6, 7}, 32)
	ciphertext := hlsTestEncrypt(t, plaintext, key, iv)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entryWriter, _ := writer.Create("segment.ts")
	_, _ = entryWriter.Write(ciphertext)
	_ = writer.Close()
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(key)
	}))
	defer origin.Close()
	segment := m3u8.Segment{Sequence: 7, Key: &m3u8.Key{Method: "AES-128", URI: origin.URL + "/key.bin"}}
	path := filepath.Join(t.TempDir(), "plain.ts")
	if err := investigationMediaCopyHLSSegment(t.Context(), reader.File[0], path, segment, ""); err == nil {
		t.Fatal("offline extraction unexpectedly fetched a key")
	}
	if requests.Load() != 0 {
		t.Fatal("offline extraction made a network request")
	}
	if err := investigationMediaCopyHLSSegment(withHLSKeyFetch(t.Context()), reader.File[0], path, segment, ""); err != nil {
		t.Fatal(err)
	}
	decoded, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(decoded, plaintext) || requests.Load() != 1 {
		t.Fatalf("ephemeral decryption failed: %v requests=%d", err, requests.Load())
	}
}

func TestHLSKeyURLRequiresReconstructableLocation(t *testing.T) {
	if _, err := resolveHLSKeyURL("https://media.example", "key.bin"); err == nil {
		t.Fatal("accepted an ambiguous historical relative key URI")
	}
	target, err := resolveHLSKeyURL("https://media.example", "/keys/key.bin")
	if err != nil || target.String() != "https://media.example/keys/key.bin" {
		t.Fatalf("root-relative key was not resolved: %v %v", target, err)
	}
}
