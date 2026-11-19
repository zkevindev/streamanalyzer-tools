package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const maxReportExplanationBytes = 256 << 10

// Report explanations are optional additions to existing archives. Unlike the
// advisory HTML, this JSON is restored only after its saved evidence is replayed.
func importedReportExplanation(dir string, report Report, entry *zip.File) error {
	if entry == nil {
		return nil
	}
	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: 汇总解读不可读", ErrInvalid)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxReportExplanationBytes+1))
	if err != nil {
		return fmt.Errorf("%w: 汇总解读不可读", ErrInvalid)
	}
	explanation, err := decodeReportBundleExplanation(data, dir, report)
	if err != nil {
		return err
	}
	return writeJSONFile(dir, explanationName, explanation)
}

func readReportBundleExplanation(dir string, report Report) (*Explanation, error) {
	file, err := os.Open(filepath.Join(dir, explanationName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReportExplanationBytes+1))
	if err != nil {
		return nil, err
	}
	return decodeReportBundleExplanation(data, dir, report)
}

func decodeReportBundleExplanation(data []byte, dir string, report Report) (*Explanation, error) {
	if len(data) == 0 || len(data) > maxReportExplanationBytes || !canonicalReportExplanationJSON(data) {
		return nil, fmt.Errorf("%w: 汇总解读格式无效", ErrInvalid)
	}
	var raw map[string]json.RawMessage
	var explanation Explanation
	if json.Unmarshal(data, &raw) != nil || json.Unmarshal(data, &explanation) != nil {
		return nil, fmt.Errorf("%w: 汇总解读格式无效", ErrInvalid)
	}
	// A case audit is never meaningful at report scope, including explicit null.
	if _, exists := raw["input_evidence"]; exists {
		return nil, fmt.Errorf("%w: 汇总解读包含单次补查输入记录", ErrInvalid)
	}
	if _, exists := raw["report_input_evidence"]; exists && explanation.ReportInputEvidence == nil {
		return nil, fmt.Errorf("%w: 汇总解读输入记录缺失", ErrInvalid)
	}
	if err := validateReportBundleExplanation(dir, report, explanation); err != nil {
		return nil, err
	}
	return &explanation, nil
}

func validateReportBundleExplanation(dir string, report Report, explanation Explanation) error {
	if explanation.InputEvidence != nil || len(explanation.Provider) > 128 || len(explanation.Model) > 256 {
		return fmt.Errorf("%w: 汇总解读型号或输入范围无效", ErrInvalid)
	}
	findings := report.Findings
	if evidence := explanation.ReportInputEvidence; evidence != nil {
		if err := validateReportModelInput(dir, report, evidence); err != nil {
			return fmt.Errorf("%w: 汇总解读输入证据与报告不一致", ErrInvalid)
		}
		findings = reportModelFindings(report, evidence)
	}
	if !validExplanation(explanation, findings) {
		return fmt.Errorf("%w: 汇总解读或证据引用无效", ErrInvalid)
	}
	return nil
}

// encoding/json accepts case-insensitive field aliases and duplicate keys.
// Evidence input records use only the canonical schema, recursively, so neither
// can hide an audit change behind a second representation of the same field.
func canonicalReportExplanationJSON(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !canonicalReportJSONValue(decoder, reflect.TypeFor[Explanation]()) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func canonicalReportJSONValue(decoder *json.Decoder, typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	if token == nil {
		return true
	}
	if typ == reflect.TypeFor[time.Time]() {
		_, ok := token.(string)
		return ok
	}
	switch typ.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return false
		}
		fields := map[string]reflect.Type{}
		canonicalReportJSONFields(typ, fields)
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			field, exists := fields[name]
			if err != nil || !ok || !exists || seen[name] {
				return false
			}
			seen[name] = true
			if !canonicalReportJSONValue(decoder, field) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case reflect.Slice, reflect.Array:
		if token != json.Delim('[') {
			return false
		}
		for decoder.More() {
			if !canonicalReportJSONValue(decoder, typ.Elem()) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		_, nested := token.(json.Delim)
		return !nested
	}
}

func canonicalReportJSONFields(typ reflect.Type, fields map[string]reflect.Type) {
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" && field.Anonymous && field.Type.Kind() == reflect.Struct {
			canonicalReportJSONFields(field.Type, fields)
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
}
