package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func playerNetworkVersionName(hash string) string { return "player-network-" + hash + ".json" }

func playerNetworkRevision(report Report) string {
	if report.Manifest.PlayerNetwork == nil {
		return ""
	}
	return report.Manifest.PlayerNetwork.SHA256
}

func readPlayerNetworkArtifact(dir string, artifact Artifact) ([]byte, error) {
	if artifact.Bytes < 0 || artifact.Bytes > maxPlayerNetworkBytes || len(artifact.SHA256) != 64 {
		return nil, ErrInvalid
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		return nil, ErrInvalid
	}
	file, err := os.Open(filepath.Join(dir, playerNetworkVersionName(artifact.SHA256)))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != artifact.Bytes {
		return nil, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPlayerNetworkBytes+1))
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if int64(len(data)) != artifact.Bytes || hex.EncodeToString(hash[:]) != artifact.SHA256 {
		return nil, ErrInvalid
	}
	return data, nil
}

func persistPlayerNetwork(dir string, report *Report, value *PlayerNetworkReport, clockOrigin time.Time) error {
	if value == nil {
		return nil
	}
	value = clonePlayerNetwork(value)
	shift := clockOrigin.Sub(report.Manifest.Observation.StartedAt).Milliseconds()
	for i := range value.Requests {
		request := &value.Requests[i]
		request.StartMS += shift
		request.EndMS += shift
		for j := range request.Hops {
			hop := &request.Hops[j]
			hop.StartMS += shift
			hop.EndMS += shift
			for k := range hop.Stages {
				hop.Stages[k].StartMS += shift
				hop.Stages[k].EndMS += shift
			}
		}
		if request.HeaderMS != nil {
			*request.HeaderMS += shift
		}
		if request.FirstBodyMS != nil {
			*request.FirstBodyMS += shift
		}
	}
	record, err := readPlayerRecording(dir)
	if err != nil {
		return err
	}
	if err := validatePlayerNetwork(value, analyzePlayer(record, report.Manifest.Observation), report.Manifest.Protocol, record.Complete); err != nil {
		return fmt.Errorf("%w: 播放器网络证据无效", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxPlayerNetworkBytes {
		return ErrInvalid
	}
	hash := sha256.Sum256(data)
	artifact := Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
	if err := writeImmutableEvidence(dir, playerNetworkVersionName(artifact.SHA256), data); err != nil {
		return err
	}
	report.Manifest.PlayerNetwork, report.PlayerNetwork = &artifact, clonePlayerNetwork(value)
	report.Manifest.Version = playerNetworkSchemaVersion
	return nil
}

func cleanupPlayerNetwork(dir string, hash string) {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		value := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "player-network-"), ".json")
		if _, err := hex.DecodeString(value); len(value) == 64 && err == nil && entry.Name() == playerNetworkVersionName(value) && value != hash {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func importedPlayerNetwork(dir string, report *Report, entries map[string]*zip.File) error {
	artifact := report.Manifest.PlayerNetwork
	if artifact == nil {
		if entries[playerNetworkName] != nil {
			return fmt.Errorf("%w: 播放器网络证据未声明", ErrInvalid)
		}
		return nil
	}
	entry := entries[playerNetworkName]
	if entry == nil || artifact.Bytes < 0 || artifact.Bytes > maxPlayerNetworkBytes || report.Player == nil || report.Manifest.Protocol != ProtocolHLS {
		return fmt.Errorf("%w: 播放器网络证据清单无效", ErrInvalid)
	}
	if err := extractArtifact(dir, entry, *artifact); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, playerNetworkName))
	if err != nil {
		return err
	}
	var value PlayerNetworkReport
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("%w: 播放器网络证据格式无效", ErrInvalid)
	}
	if !validLegacyPlayerNetworkJSON(data, value.Version) {
		return fmt.Errorf("%w: 旧版播放器网络证据包含新版字段", ErrInvalid)
	}
	record, err := readPlayerRecording(dir)
	if err != nil {
		return err
	}
	if err := validatePlayerNetwork(&value, report.Player, report.Manifest.Protocol, record.Complete); err != nil {
		return fmt.Errorf("%w: 播放器网络证据内容无效", err)
	}
	if err := writeImmutableEvidence(dir, playerNetworkVersionName(artifact.SHA256), data); err != nil {
		return err
	}
	report.PlayerNetwork = clonePlayerNetwork(&value)
	return nil
}
