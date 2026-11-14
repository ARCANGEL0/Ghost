package sessionimport

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type codexRecord struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type codexPayload struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Message   string          `json:"message"`
	Content   []codexBlock    `json:"content"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Output    string          `json:"output"`
}

type codexBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func Codex(id string) ([]Message, error) {
	path, err := findCodexSession(id)
	if err != nil {
		return nil, err
	}
	return codexFile(path)
}

func codexFile(path string) ([]Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Codex session: %w", err)
	}
	defer file.Close()

	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 64*1024), maxLineBytes)
	var imported []Message
	var totalBytes int
	for reader.Scan() {
		line := reader.Bytes()
		totalBytes += len(line)
		if totalBytes > maxImportBytes {
			return nil, errors.New("Codex session is larger than the import limit")
		}

		var record codexRecord
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		var payload codexPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			continue
		}

		var role, body string
		switch {
		case record.Type == "event_msg" && payload.Type == "user_message":
			role, body = "user", payload.Message
		case record.Type == "response_item" && payload.Type == "message":
			if payload.Role == "user" || payload.Role == "assistant" {
				role, body = payload.Role, codexText(payload.Content)
			}
		case record.Type == "response_item" && payload.Type == "function_call":
			role = "assistant"
			body = "Tool call: " + payload.Name + "\n" + string(payload.Arguments)
		case record.Type == "response_item" && payload.Type == "function_call_output":
			role = "assistant"
			body = "Tool result:\n" + payload.Output
		}

		body = strings.TrimSpace(body)
		if role == "" || body == "" {
			continue
		}
		if len(body) > maxMessageBytes {
			body = body[:maxMessageBytes] + "\n[message shortened during import]"
		}
		if len(imported) > 0 && imported[len(imported)-1].Role == role && imported[len(imported)-1].Text == body {
			continue
		}
		imported = append(imported, Message{Role: role, Text: body})
		if len(imported) > maxMessages {
			return nil, errors.New("Codex session has too many messages to import")
		}
	}
	if err := reader.Err(); err != nil {
		return nil, fmt.Errorf("read Codex session: %w", err)
	}
	if len(imported) == 0 {
		return nil, errors.New("Codex session contains no importable messages")
	}
	return imported, nil
}

func findCodexSession(id string) (string, error) {
	if len(id) > 256 || strings.ContainsAny(id, "\x00\n\r") {
		return "", fmt.Errorf("invalid Codex session reference")
	}
	codexDir := os.Getenv("CODEX_HOME")
	if codexDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find Codex session: %w", err)
		}
		codexDir = filepath.Join(homeDir, ".codex")
	}

	sessionsDir := filepath.Join(codexDir, "sessions")
	var directMatches []string
	var promptMatches []string
	err := filepath.WalkDir(sessionsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == sessionsDir {
				return walkErr
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if sessionIDPattern.MatchString(id) {
			if strings.HasSuffix(entry.Name(), "-"+id+".jsonl") {
				directMatches = append(directMatches, path)
			}
		}
		prompt := firstCodexPrompt(path)
		if prompt != "" && strings.Contains(strings.ToLower(prompt), strings.ToLower(id)) {
			promptMatches = append(promptMatches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("find Codex session: %w", err)
	}
	if len(directMatches) > 1 {
		return "", fmt.Errorf("Codex session ID %q matches more than one file", id)
	}
	if len(directMatches) == 1 {
		return directMatches[0], nil
	}
	if len(promptMatches) > 1 {
		return "", fmt.Errorf("more than one Codex session matches %q; use its session ID", id)
	}
	if len(promptMatches) == 0 {
		return "", fmt.Errorf("Codex session %q was not found under %s", id, sessionsDir)
	}
	return promptMatches[0], nil
}

func firstCodexPrompt(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 64*1024), maxLineBytes)
	for reader.Scan() {
		var record codexRecord
		if json.Unmarshal(reader.Bytes(), &record) != nil {
			continue
		}
		var payload codexPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			continue
		}
		if record.Type == "event_msg" && payload.Type == "user_message" {
			return strings.TrimSpace(payload.Message)
		}
		if record.Type == "response_item" && payload.Type == "message" && payload.Role == "user" {
			return strings.TrimSpace(codexText(payload.Content))
		}
	}
	return ""
}

func codexText(blocks []codexBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "input_text" || block.Type == "output_text" || block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}
