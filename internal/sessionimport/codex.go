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
	if !sessionIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid Codex session ID %q", id)
	}

	path, err := findCodexSession(id)
	if err != nil {
		return nil, err
	}
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
	codexDir := os.Getenv("CODEX_HOME")
	if codexDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find Codex session: %w", err)
		}
		codexDir = filepath.Join(homeDir, ".codex")
	}

	sessionsDir := filepath.Join(codexDir, "sessions")
	var found string
	err := filepath.WalkDir(sessionsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == sessionsDir {
				return walkErr
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-"+id+".jsonl") {
			return nil
		}
		if found != "" {
			return fmt.Errorf("Codex session ID %q matches more than one file", id)
		}
		found = path
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("find Codex session: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("Codex session %q was not found under %s", id, sessionsDir)
	}
	return found, nil
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
