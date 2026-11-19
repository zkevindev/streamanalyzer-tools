package hlsorigin

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"net/http"
	"time"
)

const keyRotationSequence = 6

var ErrKeyRestoreUnavailable = errors.New("当前 HLS 源没有待恢复的密钥")

type KeyRotationStatus struct {
	RotationSequence uint64    `json:"rotation_sequence"`
	K1Requests       uint64    `json:"k1_requests"`
	K2Requests       uint64    `json:"k2_requests"`
	K2Denied         uint64    `json:"k2_denied"`
	K2Allowed        uint64    `json:"k2_allowed"`
	RestoredAt       time.Time `json:"restored_at,omitzero"`
}

func keyIndex(sequence uint64) int {
	if sequence >= keyRotationSequence {
		return 1
	}
	return 0
}

func encryptSegment(data []byte, key [aes.BlockSize]byte, sequence uint64) []byte {
	block, _ := aes.NewCipher(key[:])
	padding := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
	var iv [aes.BlockSize]byte
	binary.BigEndian.PutUint64(iv[8:], sequence)
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(data, data)
	return data
}

func (m *Manager) RestoreKey(id string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.Active || m.status.ID != id || m.status.Mode != "key_forbidden" {
		return Status{}, ErrKeyRestoreUnavailable
	}
	if m.status.KeyRotation.RestoredAt.IsZero() {
		m.status.KeyRotation.RestoredAt = time.Now()
	}
	return m.snapshot(), nil
}

// The caller holds m.mu. Snapshot the key before releasing it so a stopped
// source cannot accidentally begin serving the next source's key.
func (m *Manager) serveKey(w http.ResponseWriter, r *http.Request, index int) {
	status := m.status.KeyRotation
	denied := index == 1 && m.status.Mode == "key_forbidden" && status.RestoredAt.IsZero()
	if r.Method == http.MethodGet {
		switch {
		case index == 0:
			status.K1Requests++
		case denied:
			status.K2Requests++
			status.K2Denied++
			m.status.FaultHits++
			now := time.Now()
			m.status.LastFault = &now
		default:
			status.K2Requests++
			status.K2Allowed++
		}
	}
	key := m.keys[index]
	m.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if denied {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(key[:]))
}
