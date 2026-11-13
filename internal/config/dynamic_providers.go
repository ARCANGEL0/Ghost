package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	freeProviders "github.com/swadhinbiswas/ghost/internal/config/providers"
	"github.com/swadhinbiswas/ghost/internal/env"
)

// httpClient is reused across dynamic fetches.
var httpClient = &http.Client{Timeout: 15 * time.Second}

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
	case "openrouter", "kilo", "nvidia-nim", "groq", "cerebras", "googleai", "cloudflare", "cohere", "amd-radeon", "aion", "huggingface", "llm7", "typhoon", "nous-portal", "ollama-local":
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
