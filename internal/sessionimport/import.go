package sessionimport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Source struct {
	ID   string
	Name string
}

type Result struct {
	ID       string
	Title    string
	Provider string
	Messages []Message
}

func Sources() []Source {
	return []Source{
		{ID: "claude", Name: "Claude Code"},
		{ID: "codex", Name: "Codex"},
		{ID: "opencode", Name: "OpenCode"},
		{ID: "kilo", Name: "Kilo"},
		{ID: "file", Name: "Transcript file"},
	}
}

func SourceName(id string) string {
	for _, source := range Sources() {
		if source.ID == id {
			return source.Name
		}
	}
	return id
}

func Import(ctx context.Context, source, reference, workingDir string) (Result, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return Result{}, errors.New("enter a session ID, title, or transcript path")
	}

	switch source {
	case "claude":
		path, err := findClaudeSession(reference)
		if err != nil {
			return Result{}, err
		}
		messages, err := claudeFile(path)
		if err != nil {
			return Result{}, err
		}
		return imported("claude-code", strings.TrimSuffix(filepath.Base(path), ".jsonl"), messages), nil
	case "codex":
		path, err := findCodexSession(reference)
		if err != nil {
			return Result{}, err
		}
		messages, err := codexFile(path)
		if err != nil {
			return Result{}, err
		}
		id := codexSessionID(path, reference)
		return imported("codex", id, messages), nil
	case "opencode", "kilo":
		return importCLI(ctx, source, reference, workingDir)
	case "file":
		if !filepath.IsAbs(reference) && workingDir != "" {
			reference = filepath.Join(workingDir, reference)
		}
		return importFile(reference)
	default:
		return Result{}, fmt.Errorf("unsupported session source %q", source)
	}
}

func importCLI(ctx context.Context, source, reference, workingDir string) (Result, error) {
	bin := source
	listArgs := []string{"session", "list", "--max-count", "200", "--format", "json"}
	if source == "kilo" {
		listArgs = []string{"session", "list", "--all", "--max-count", "200", "--format", "json"}
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%s CLI was not found on PATH", source)
	}

	listData, err := runCLI(ctx, bin, listArgs, workingDir)
	if err != nil {
		return Result{}, fmt.Errorf("list %s sessions: %w", source, err)
	}
	id, title, err := findCLIReference(listData, reference)
	if err != nil {
		return Result{}, err
	}
	if id == "" {
		id = reference
	}

	exportArgs := []string{"session", "export", id}
	if source == "kilo" {
		exportArgs = []string{"export", id}
	}
	data, err := runCLI(ctx, bin, exportArgs, workingDir)
	if err != nil {
		return Result{}, fmt.Errorf("export %s session %q: %w", source, id, err)
	}
	messages, exportTitle, err := parseExport(data)
	if err != nil {
		return Result{}, fmt.Errorf("read %s session export: %w", source, err)
	}
	if title == "" {
		title = exportTitle
	}
	result := imported(source, id, messages)
	if title != "" {
		result.Title = title
	}
	return result, nil
}

func runCLI(ctx context.Context, bin string, args []string, workingDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, errors.New(strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	if len(data) > maxImportBytes {
		return nil, errors.New("session export is larger than the import limit")
	}
	return data, nil
}

func findCLIReference(data []byte, reference string) (string, string, error) {
	var root any
	if json.Unmarshal(data, &root) != nil {
		return "", "", errors.New("the session list was not valid JSON")
	}
	entries := make([]map[string]any, 0)
	collectEntries(root, &entries)

	query := strings.ToLower(strings.TrimSpace(reference))
	var matches []map[string]any
	for _, entry := range entries {
		id := firstString(entry, "id", "sessionId", "session_id")
		title := firstString(entry, "title", "name", "summary")
		if id == reference {
			return id, title, nil
		}
		if title != "" && strings.Contains(strings.ToLower(title), query) {
			matches = append(matches, entry)
		}
	}
	if len(matches) > 1 {
		return "", "", fmt.Errorf("more than one session matches %q; use its ID", reference)
	}
	if len(matches) == 1 {
		return firstString(matches[0], "id", "sessionId", "session_id"), firstString(matches[0], "title", "name", "summary"), nil
	}
	return "", "", nil
}

func collectEntries(value any, entries *[]map[string]any) {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			collectEntries(item, entries)
		}
	case map[string]any:
		if firstString(value, "id", "sessionId", "session_id") != "" {
			*entries = append(*entries, value)
		}
		for _, key := range []string{"sessions", "data", "items", "results"} {
			if child, ok := value[key]; ok {
				collectEntries(child, entries)
			}
		}
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func parseExport(data []byte) ([]Message, string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, "", err
	}
	var info map[string]json.RawMessage
	_ = json.Unmarshal(root["info"], &info)
	title := rawString(info["title"])
	if title == "" {
		title = rawString(root["title"])
	}
	var rawMessages []json.RawMessage
	if err := json.Unmarshal(root["messages"], &rawMessages); err != nil {
		return nil, "", errors.New("export did not contain a message list")
	}
	messages := make([]Message, 0, len(rawMessages))
	for _, raw := range rawMessages {
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		var messageInfo map[string]json.RawMessage
		_ = json.Unmarshal(item["info"], &messageInfo)
		role := rawString(messageInfo["role"])
		if role == "" {
			role = rawString(item["role"])
		}
		if role != "user" && role != "assistant" {
			continue
		}
		text := rawString(item["content"])
		if text == "" {
			text = exportParts(item["parts"])
		}
		if text == "" {
			text = exportParts(item["content"])
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if len(text) > maxMessageBytes {
			text = text[:maxMessageBytes] + "\n[message shortened during import]"
		}
		messages = append(messages, Message{Role: role, Text: text})
		if len(messages) > maxMessages {
			return nil, "", errors.New("session has too many messages to import")
		}
	}
	if len(messages) == 0 {
		return nil, "", errors.New("session export contains no readable messages")
	}
	return messages, title, nil
}

func exportParts(raw json.RawMessage) string {
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, part := range parts {
		switch rawString(part["type"]) {
		case "text", "input_text", "output_text":
			if text := rawString(part["text"]); text != "" {
				out = append(out, text)
			}
		case "tool", "tool_use", "tool_call":
			name := rawString(part["name"])
			if name != "" {
				out = append(out, "Tool call: "+name)
			}
		}
	}
	return strings.Join(out, "\n")
}

func importFile(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read transcript file: %w", err)
	}
	if len(data) > maxImportBytes {
		return Result{}, errors.New("transcript file is larger than the import limit")
	}
	if messages, title, err := parseExport(data); err == nil {
		result := imported("transcript", filepath.Base(path), messages)
		if title != "" {
			result.Title = title
		}
		return result, nil
	}
	if messages, err := parseJSONLines(data); err == nil {
		return imported("transcript", filepath.Base(path), messages), nil
	}
	if strings.TrimSpace(string(data)) == "" {
		return Result{}, errors.New("transcript file is empty")
	}
	text := strings.TrimSpace(string(data))
	if len(text) > maxImportBytes {
		return Result{}, errors.New("transcript file is larger than the import limit")
	}
	return imported("transcript", filepath.Base(path), []Message{{Role: "user", Text: "Imported transcript:\n\n" + text}}), nil
}

func parseJSONLines(data []byte) ([]Message, error) {
	reader := bufio.NewScanner(bytes.NewReader(data))
	reader.Buffer(make([]byte, 64*1024), maxLineBytes)
	var messages []Message
	for reader.Scan() {
		line := bytes.TrimSpace(reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var item map[string]json.RawMessage
		if json.Unmarshal(line, &item) != nil {
			return nil, errors.New("not JSON lines")
		}
		role := rawString(item["role"])
		text := rawString(item["text"])
		if text == "" {
			text = rawString(item["content"])
		}
		if text == "" {
			text = rawString(item["message"])
		}
		if role != "user" && role != "assistant" || strings.TrimSpace(text) == "" {
			continue
		}
		messages = append(messages, Message{Role: role, Text: strings.TrimSpace(text)})
		if len(messages) > maxMessages {
			return nil, errors.New("transcript has too many messages to import")
		}
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("transcript contains no readable messages")
	}
	return messages, nil
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func imported(provider, id string, messages []Message) Result {
	title := ""
	for _, item := range messages {
		if item.Role == "user" {
			title = item.Text
			break
		}
	}
	if title == "" {
		title = id
	}
	if len(title) > 64 {
		title = strings.TrimSpace(title[:64]) + "..."
	}
	return Result{ID: id, Title: title, Provider: provider, Messages: messages}
}

func codexSessionID(path, fallback string) string {
	file, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer file.Close()
	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 64*1024), maxLineBytes)
	for reader.Scan() {
		var record codexRecord
		if json.Unmarshal(reader.Bytes(), &record) != nil || record.Type != "session_meta" {
			continue
		}
		var payload codexPayload
		if json.Unmarshal(record.Payload, &payload) == nil && payload.ID != "" {
			return payload.ID
		}
	}
	return fallback
}

func sessionIDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}
