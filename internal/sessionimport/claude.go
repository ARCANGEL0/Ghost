package sessionimport

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxLineBytes    = 64 << 20
	maxImportBytes  = 128 << 20
	maxMessageBytes = 256 << 10
	maxMessages     = 10000
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Message struct {
	Role string
	Text string
}

type claudeRecord struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

func Claude(id string) ([]Message, error) {
	path, err := findClaudeSession(id)
	if err != nil {
		return nil, err
	}
	return claudeFile(path)
}

func claudeFile(path string) ([]Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Claude Code session: %w", err)
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
			return nil, errors.New("Claude Code session is larger than the import limit")
		}

		var record claudeRecord
		if err := json.Unmarshal(line, &record); err != nil || record.IsSidechain || record.IsMeta {
			continue
		}
		role := record.Message.Role
		if role == "" {
			role = record.Type
		}
		if role != "user" && role != "assistant" {
			continue
		}

		text := strings.TrimSpace(readContent(record.Message.Content, 0))
		if text == "" {
			continue
		}
		if len(text) > maxMessageBytes {
			text = text[:maxMessageBytes] + "\n[message shortened during import]"
		}
		imported = append(imported, Message{Role: role, Text: text})
		if len(imported) > maxMessages {
			return nil, errors.New("Claude Code session has too many messages to import")
		}
	}
	if err := reader.Err(); err != nil {
		return nil, fmt.Errorf("read Claude Code session: %w", err)
	}
	if len(imported) == 0 {
		return nil, errors.New("Claude Code session contains no importable messages")
	}
	return imported, nil
}

func findClaudeSession(id string) (string, error) {
	if len(id) > 256 || strings.ContainsAny(id, "\x00\n\r") {
		return "", fmt.Errorf("invalid Claude Code session reference")
	}
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find Claude Code config: %w", err)
		}
		configDir = filepath.Join(homeDir, ".claude")
	}

	projectsDir := filepath.Join(configDir, "projects")
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", fmt.Errorf("read Claude Code projects: %w", err)
	}

	var directMatches []string
	var promptMatches []string
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(projectsDir, project.Name())
		if sessionIDPattern.MatchString(id) {
			path := filepath.Join(projectDir, id+".jsonl")
			info, err := os.Stat(path)
			if err == nil && info.Mode().IsRegular() {
				directMatches = append(directMatches, path)
			}
		}
		files, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, entry := range files {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(projectDir, entry.Name())
			prompt := firstClaudePrompt(path)
			if prompt != "" && strings.Contains(strings.ToLower(prompt), strings.ToLower(id)) {
				promptMatches = append(promptMatches, path)
			}
		}
	}
	if len(directMatches) > 1 {
		return "", fmt.Errorf("Claude Code session ID %q matches more than one file", id)
	}
	if len(directMatches) == 1 {
		return directMatches[0], nil
	}
	matches := promptMatches
	if len(matches) > 1 {
		return "", fmt.Errorf("more than one Claude Code session matches %q; use its session ID", id)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("Claude Code session %q was not found under %s", id, projectsDir)
	}
	return matches[0], nil
}

func firstClaudePrompt(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 64*1024), maxLineBytes)
	for reader.Scan() {
		var record claudeRecord
		if json.Unmarshal(reader.Bytes(), &record) != nil || record.IsSidechain || record.IsMeta {
			continue
		}
		role := record.Message.Role
		if role == "" {
			role = record.Type
		}
		if role == "user" {
			return strings.TrimSpace(readContent(record.Message.Content, 0))
		}
	}
	return ""
}

func readContent(raw json.RawMessage, depth int) string {
	if depth > 4 || len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}

	var parts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "tool_use":
			input := strings.TrimSpace(string(block.Input))
			if input != "" && input != "null" {
				parts = append(parts, "Tool call: "+block.Name+"\n"+input)
			}
		case "tool_result":
			result := strings.TrimSpace(readContent(block.Content, depth+1))
			if result != "" {
				label := "Tool result"
				if block.IsError {
					label = "Tool error"
				}
				parts = append(parts, label+":\n"+result)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
