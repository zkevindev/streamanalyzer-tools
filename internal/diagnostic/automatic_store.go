package diagnostic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func automaticVersionName(hash string) string { return "automatic-investigation-" + hash + ".json" }

func readAutomaticInvestigation(dir string, artifact Artifact) ([]byte, error) {
	hash, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(hash) != sha256.Size || artifact.Bytes < 1 || artifact.Bytes > maxAutomaticBytes {
		return nil, ErrInvalid
	}
	file, err := os.Open(filepath.Join(dir, automaticVersionName(artifact.SHA256)))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAutomaticBytes+1))
	if err != nil || int64(len(data)) != artifact.Bytes {
		return nil, ErrInvalid
	}
	actual := sha256.Sum256(data)
	if hex.EncodeToString(actual[:]) != artifact.SHA256 {
		return nil, ErrInvalid
	}
	return data, nil
}

// The caller holds persistence.Lock. Preparing an immutable sidecar first lets
// a case ledger and its automatic case-ID binding share the report commit.
func attachAutomaticInvestigation(dir string, report *Report, state AutomaticInvestigation) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxAutomaticBytes {
		return fmt.Errorf("%w: 自动补查记录超限", ErrInvalid)
	}
	hash := sha256.Sum256(data)
	artifact := Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
	if err := writeImmutableEvidence(dir, automaticVersionName(artifact.SHA256), data); err != nil {
		return err
	}
	var snapshot AutomaticInvestigation
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	report.Manifest.AutomaticInvestigation = &artifact
	report.AutomaticInvestigation = &snapshot
	return nil
}

// Automatic status updates preserve the whole-report result: its input scope
// intentionally excludes task progress and this sidecar's own artifact hash.
func persistAutomaticInvestigation(dir string, report *Report, state AutomaticInvestigation) error {
	if err := attachAutomaticInvestigation(dir, report, state); err != nil {
		return err
	}
	return persistReport(dir, *report)
}
