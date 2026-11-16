package remote

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/swadhinbiswas/ghost/internal/config"
)

type tokenFile struct {
	Token string `json:"token"`
}

// LoadToken returns the supplied token or reads and creates the saved token.
func LoadToken(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	path := filepath.Join(filepath.Dir(config.GlobalConfig()), "remote.json")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("remote token file is not a regular file: %s", path)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("secure remote token file: %w", err)
		}
		return readToken(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check remote token file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate remote token: %w", err)
	}
	value := tokenFile{Token: base64.RawURLEncoding.EncodeToString(raw)}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode remote token: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readToken(path)
	}
	if err != nil {
		return "", fmt.Errorf("create remote token file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write remote token file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close remote token file: %w", err)
	}
	return value.Token, nil
}

func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read remote token file: %w", err)
	}
	var value tokenFile
	if err := json.Unmarshal(data, &value); err != nil || value.Token == "" {
		return "", fmt.Errorf("remote token file is invalid: %s", path)
	}
	return value.Token, nil
}
