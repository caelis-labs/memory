package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxEvaluationSourceBytes = 512 << 20
	maxEvaluationLineBytes   = 16 << 20
	maxEvaluationTextBytes   = 16 << 10
)

type sourceData struct {
	kind       string
	digest     string
	bytes      int64
	extracted  int
	duplicates int
	skipped    int
	chunks     []string
}

type codexEvent struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"payload"`
}

type codexMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type caelisEvent struct {
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
	Message    *struct {
		Role  string `json:"role"`
		Parts []struct {
			Kind string          `json:"kind"`
			Text json.RawMessage `json:"text"`
		} `json:"parts"`
	} `json:"message"`
}

func loadSource(path, requestedKind string) (sourceData, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return sourceData{}, fmt.Errorf("inspect source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return sourceData{}, fmt.Errorf("source must be a regular file")
	}
	if info.Size() > maxEvaluationSourceBytes {
		return sourceData{}, fmt.Errorf("source exceeds %d bytes", maxEvaluationSourceBytes)
	}
	kind, err := resolveSourceKind(path, requestedKind)
	if err != nil {
		return sourceData{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return sourceData{}, fmt.Errorf("open source: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	reader := io.TeeReader(file, hash)
	data := sourceData{kind: kind, bytes: info.Size(), chunks: make([]string, 0)}
	seen := make(map[[sha256.Size]byte]struct{})
	add := func(text string) {
		data.extracted++
		normalized, ok := normalizeCandidate(text)
		if !ok {
			data.skipped++
			return
		}
		digest := sha256.Sum256([]byte(normalized))
		if _, exists := seen[digest]; exists {
			data.duplicates++
			return
		}
		seen[digest] = struct{}{}
		data.chunks = append(data.chunks, normalized)
	}
	switch kind {
	case "markdown":
		err = scanMarkdown(reader, add)
	case "codex-jsonl":
		err = scanCodexJSONL(reader, add)
	case "caelis-jsonl":
		err = scanCaelisJSONL(reader, add)
	default:
		return sourceData{}, fmt.Errorf("unsupported source format")
	}
	if err != nil {
		return sourceData{}, err
	}
	data.digest = hex.EncodeToString(hash.Sum(nil))
	return data, nil
}

func resolveSourceKind(path, requested string) (string, error) {
	switch requested {
	case "markdown", "codex-jsonl", "caelis-jsonl":
		return requested, nil
	case "auto":
		if strings.EqualFold(filepath.Ext(path), ".jsonl") {
			return "codex-jsonl", nil
		}
		return "markdown", nil
	default:
		return "", fmt.Errorf("-format must be auto, markdown, codex-jsonl, or caelis-jsonl")
	}
}

func scanCaelisJSONL(reader io.Reader, add func(string)) error {
	scanner := newEvaluationScanner(reader)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var event caelisEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode Caelis JSONL event %d: %w", lineNumber, err)
		}
		if event.Visibility != "canonical" || event.Message == nil ||
			(event.Type != "user" && event.Type != "assistant") ||
			(event.Message.Role != "user" && event.Message.Role != "assistant") {
			continue
		}
		for _, part := range event.Message.Parts {
			if part.Kind != "text" {
				continue
			}
			text, err := decodeCaelisText(part.Text)
			if err != nil {
				return fmt.Errorf("decode Caelis JSONL text %d: %w", lineNumber, err)
			}
			for _, line := range strings.Split(text, "\n") {
				add(line)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan Caelis JSONL source: %w", err)
	}
	return nil
}

func decodeCaelisText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var envelope struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Text == "" {
		return "", fmt.Errorf("unsupported canonical text part")
	}
	return envelope.Text, nil
}

func scanMarkdown(reader io.Reader, add func(string)) error {
	scanner := newEvaluationScanner(reader)
	inFence := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			add(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan Markdown source: %w", err)
	}
	return nil
}

func scanCodexJSONL(reader io.Reader, add func(string)) error {
	scanner := newEvaluationScanner(reader)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var event codexEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode JSONL event %d: %w", lineNumber, err)
		}
		if event.Type != "response_item" || event.Payload.Type != "message" ||
			(event.Payload.Role != "user" && event.Payload.Role != "assistant") {
			continue
		}
		var contents []codexMessageContent
		if err := json.Unmarshal(event.Payload.Content, &contents); err != nil {
			return fmt.Errorf("decode JSONL message content %d: %w", lineNumber, err)
		}
		for _, content := range contents {
			if content.Type != "input_text" && content.Type != "output_text" {
				continue
			}
			for _, line := range strings.Split(content.Text, "\n") {
				add(line)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan Codex JSONL source: %w", err)
	}
	return nil
}

func newEvaluationScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxEvaluationLineBytes)
	return scanner
}

func normalizeCandidate(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for value != "" {
		switch value[0] {
		case '#', '>', '-', '*':
			value = strings.TrimSpace(value[1:])
		default:
			goto prefixesDone
		}
	}
prefixesDone:
	if separator := strings.IndexByte(value, '.'); separator > 0 && separator < 5 {
		if _, err := strconv.Atoi(value[:separator]); err == nil {
			value = strings.TrimSpace(value[separator+1:])
		}
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 8 || len(value) > maxEvaluationTextBytes {
		return "", false
	}
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") && !strings.Contains(value, " ") {
		return "", false
	}
	return strings.Join(strings.Fields(value), " "), true
}
