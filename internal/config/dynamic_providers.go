package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	freeProviders "github.com/swadhinbiswas/ghost/internal/config/providers"
	"github.com/swadhinbiswas/ghost/internal/env"
)

// httpClient is reused across dynamic fetches.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// openCodeModelsResponse matches the OpenCode /models endpoint.
type openCodeModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

var openCodeLargeModelPreferences = []string{
	"big-pickle",
}

var openCodeSmallModelPreferences = []string{
	"deepseek-v4-flash-free",
	"mimo-v2.5-free",
	"qwen3.6-plus-free",
	"north-mini-code-free",
}

var openCodeDisplayNameOverrides = map[string]string{
	"big-pickle":             "Big Pickle",
	"deepseek-v4-flash-free": "DeepSeek V4 Flash Free",
	"mimo-v2.5-free":         "Xiaomi MiMo V2.5 Free",
	"qwen3.6-plus-free":      "Qwen 3.6 Plus Free",
	"minimax-m3-free":        "MiniMax M3 Free",
	"nemotron-3-ultra-free":  "Nemotron 3 Ultra Free",
	"north-mini-code-free":   "North Mini Code Free",
}

// isOpenCodeFreeModel reports whether an OpenCode Zen model ID is free.
// Free models are suffixed "-free"; "big-pickle" is a special stealth free model.
func isOpenCodeFreeModel(id string) bool {
	return id == "big-pickle" || strings.HasSuffix(id, "-free")
}

// openCodeModelsURL is the OpenCode Zen model list endpoint.
const openCodeModelsURL = "https://opencode.ai/zen/v1/models"

// openCodeModelsEndpoint returns the configured OpenCode Zen models URL,
// honoring the GHOST_OPENCODE_MODELS_URL override (used for testing/forks).
func openCodeModelsEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("GHOST_OPENCODE_MODELS_URL")); v != "" {
		return v
	}
	return openCodeModelsURL
}

// fetchOpenCodeModels returns the models exposed by the OpenCode Zen model list.
func fetchOpenCodeModels() ([]catwalk.Model, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openCodeModelsEndpoint(), nil)
	if err != nil {
		return nil, fmt.Errorf("opencode: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OpenCode/1.0.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode: fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencode: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var parsed openCodeModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("opencode: decode response: %w", err)
	}

	var (
		models []catwalk.Model
		seen   = make(map[string]struct{})
	)
	for _, m := range parsed.Data {
		if m.ID == "" {
			continue
		}
		if !isOpenCodeFreeModel(m.ID) {
			continue
		}
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}
		models = append(models, catwalk.Model{
			ID:               m.ID,
			Name:             displayNameFromOpenCodeID(m.ID),
			ContextWindow:    128000,
			DefaultMaxTokens: 8192,
		})
	}

	sortOpenCodeModels(models)
	return models, nil
}

func sortOpenCodeModels(models []catwalk.Model) {
	priority := func(id string) int {
		for i, preferred := range append(openCodeLargeModelPreferences, openCodeSmallModelPreferences...) {
			if id == preferred {
				return i
			}
		}
		if strings.Contains(id, "free") {
			return len(openCodeLargeModelPreferences) + len(openCodeSmallModelPreferences)
		}
		return len(openCodeLargeModelPreferences) + len(openCodeSmallModelPreferences) + 1
	}

	sort.SliceStable(models, func(i, j int) bool {
		pi := priority(models[i].ID)
		pj := priority(models[j].ID)
		if pi != pj {
			return pi < pj
		}
		return models[i].ID < models[j].ID
	})
}

func displayNameFromOpenCodeID(id string) string {
	if name, ok := openCodeDisplayNameOverrides[id]; ok {
		return name
	}
	return displayNameFromID(id)
}

func pickOpenCodeModelID(models []catwalk.Model, preferredIDs []string) string {
	if len(models) == 0 {
		return ""
	}

	lookup := make(map[string]struct{}, len(models))
	for _, model := range models {
		lookup[model.ID] = struct{}{}
	}
	for _, preferredID := range preferredIDs {
		if _, ok := lookup[preferredID]; ok {
			return preferredID
		}
	}

	return models[0].ID
}

func pickOpenCodeSmallModelID(models []catwalk.Model, largeModelID string) string {
	if len(models) == 0 {
		return ""
	}

	lookup := make(map[string]struct{}, len(models))
	for _, model := range models {
		lookup[model.ID] = struct{}{}
	}
	for _, preferredID := range openCodeSmallModelPreferences {
		if preferredID == largeModelID {
			continue
		}
		if _, ok := lookup[preferredID]; ok {
			return preferredID
		}
	}

	for _, model := range models {
		if model.ID != largeModelID {
			return model.ID
		}
	}

	return largeModelID
}

// displayNameFromID converts a model ID like "minimax-m2.5-free" into "MiniMax M2.5 Free".
func displayNameFromID(id string) string {
	// Strip known suffixes.
	id = strings.TrimSuffix(id, ":free")
	id = strings.TrimSuffix(id, "-free")

	// Replace common separators with spaces.
	s := strings.ReplaceAll(id, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "/", " ")

	// Title-case each word.
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

// fetchDynamicProviders returns providers whose model lists are fetched live from APIs.
// Errors are logged and the provider is omitted if the fetch fails.
// fetchDynamicProviders returns providers whose model lists are cached locally.
func fetchDynamicProviders() []catwalk.Provider {
	// Try loading from cache first to avoid slow network lookups on startup
	if cached, err := loadDynamicProvidersFromCache(); err == nil && len(cached) > 0 {
		return cached
	}

	// Fallback to static embedded/local providers if cache is empty/invalid
	return getStaticDynamicProvidersFallback()
}

// fetchFreshDynamicProviders fetches dynamic providers remotely. Each source is
// best-effort: a failure is logged and that provider is simply omitted from the
// fresh set (callers fall back to cache or embedded defaults).
func fetchFreshDynamicProviders(cfgs ...*Config) []catwalk.Provider {
	var cfg *Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	base := freeProviders.AllFreeProviders()
	results := make(chan catwalk.Provider, len(base))
	var wg sync.WaitGroup

	for _, provider := range base {
		provider := provider
		if !hasLiveCatalog(string(provider.ID)) {
			continue
		}
		if cfg != nil && cfg.Providers != nil {
			if saved, ok := cfg.Providers.Get(string(provider.ID)); ok && saved.BaseURL != "" && provider.ID != "cloudflare" {
				provider.APIEndpoint = saved.BaseURL
			}
		}
		key := providerCatalogKey(cfg, provider)
		if needsCatalogKey(string(provider.ID)) && key == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			fresh, err := fetchProviderCatalog(provider, key, cfg)
			if err != nil {
				slog.Warn("Failed to refresh provider catalog", "provider", provider.ID, "error", err)
				return
			}
			if len(fresh.Models) > 0 {
				results <- fresh
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var out []catwalk.Provider
	for provider := range results {
		out = append(out, provider)
	}
	return out
}

func hasLiveCatalog(id string) bool {
	switch id {
	case "opencode", "opencode-zen", "openrouter", "kilo", "nvidia-nim", "groq", "cerebras", "googleai", "cloudflare", "cohere", "amd-radeon", "aion", "huggingface", "llm7", "typhoon", "nous-portal", "ollama-local":
		return true
	default:
		return false
	}
}

func needsCatalogKey(id string) bool {
	switch id {
	case "nvidia-nim", "groq", "cerebras", "googleai", "cloudflare", "cohere", "amd-radeon", "huggingface", "llm7", "typhoon", "nous-portal":
		return true
	default:
		return false
	}
}

func providerCatalogKey(cfg *Config, provider catwalk.Provider) string {
	key := provider.APIKey
	if cfg != nil && cfg.Providers != nil {
		if saved, ok := cfg.Providers.Get(string(provider.ID)); ok && saved.APIKey != "" {
			key = saved.APIKey
		}
	}
	if key == "" || key == "no-key-needed" {
		return ""
	}
	value, err := NewShellVariableResolver(env.New()).ResolveValue(key)
	if err != nil {
		return ""
	}
	if value == "no-key-needed" {
		return ""
	}
	return value
}

func fetchProviderCatalog(provider catwalk.Provider, key string, cfg *Config) (catwalk.Provider, error) {
	id := string(provider.ID)
	var models []catwalk.Model
	var err error
	switch id {
	case "opencode", "opencode-zen":
		models, err = fetchOpenCodeModels()
	case "nvidia-nim":
		return fetchNvidiaNimProviderWithKey(key)
	case "googleai":
		models, err = fetchGoogleModelCatalog(provider.APIEndpoint, key)
	case "cloudflare":
		account := ""
		if cfg != nil && cfg.Providers != nil {
			if saved, ok := cfg.Providers.Get(id); ok {
				account = saved.ExtraParams["account_id"]
			}
		}
		if account == "" {
			account = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
		models, err = fetchCloudflareModelCatalog(account, key)
	case "cohere":
		models, err = fetchCohereModelCatalog(key)
	case "amd-radeon":
		models, err = fetchAmdModelCatalog(provider.APIEndpoint, key)
	case "llm7":
		models, err = fetchLLM7ModelCatalog(provider.APIEndpoint, key)
	case "typhoon":
		models, err = fetchTyphoonModelCatalog(provider.APIEndpoint, key)
	case "nous-portal":
		models, err = fetchFreeOpenAIModelCatalog(provider.APIEndpoint, key)
	default:
		models, err = fetchOpenAIModelCatalog(provider.APIEndpoint, catalogKeyFor(id, key))
	}
	if err != nil {
		return catwalk.Provider{}, fmt.Errorf("%s: %w", id, err)
	}

	filtered := models[:0]
	for _, model := range models {
		switch id {
		case "openrouter":
			if !strings.HasSuffix(model.ID, ":free") {
				continue
			}
		case "kilo":
			if !strings.HasSuffix(model.ID, ":free") && model.ID != "kilo-auto/free" {
				continue
			}
		case "opencode", "opencode-zen":
			if !isOpenCodeFreeModel(model.ID) {
				continue
			}
		case "googleai":
			if !isGoogleFreeModel(model.ID) {
				continue
			}
		}
		filtered = append(filtered, model)
	}
	if id == "openrouter" {
		found := false
		for _, model := range filtered {
			if model.ID == "openrouter/free" {
				found = true
				break
			}
		}
		if !found {
			filtered = append(filtered, catwalk.Model{
				ID:               "openrouter/free",
				Name:             "OpenRouter Free Auto",
				ContextWindow:    128_000,
				DefaultMaxTokens: 8192,
			})
		}
	}
	if id == "kilo" {
		found := false
		for _, model := range filtered {
			if model.ID == "kilo-auto/free" {
				found = true
				break
			}
		}
		if !found {
			filtered = append(filtered, catwalk.Model{
				ID:               "kilo-auto/free",
				Name:             "Kilo Auto Free",
				ContextWindow:    128_000,
				DefaultMaxTokens: 8192,
			})
		}
	}
	if len(filtered) == 0 {
		return catwalk.Provider{}, fmt.Errorf("catalog has no supported models")
	}
	if id == "opencode" || id == "opencode-zen" {
		sortOpenCodeModels(filtered)
	}
	provider.Models = filtered
	provider.DefaultLargeModelID = liveDefault(provider.DefaultLargeModelID, filtered)
	provider.DefaultSmallModelID = liveDefault(provider.DefaultSmallModelID, filtered)
	return provider, nil
}

func FetchProviderCatalog(provider catwalk.Provider, key string) (catwalk.Provider, error) {
	return fetchProviderCatalog(provider, key, nil)
}

func catalogKeyFor(id, key string) string {
	if id == "openrouter" || id == "kilo" || id == "ollama-local" {
		return ""
	}
	return key
}

func liveDefault(preferred string, models []catwalk.Model) string {
	for _, model := range models {
		if model.ID == preferred {
			return preferred
		}
	}
	return models[0].ID
}

func saveDynamicProvidersToCache(providers []catwalk.Provider) error {
	path := cachePathFor("dynamic_providers")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadDynamicProvidersFromCache() ([]catwalk.Provider, error) {
	path := cachePathFor("dynamic_providers")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var providers []catwalk.Provider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func getStaticDynamicProvidersFallback() []catwalk.Provider {
	return freeProviders.AllFreeProviders()
}
