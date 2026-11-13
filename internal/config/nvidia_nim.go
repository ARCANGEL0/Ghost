package config

import (
	"fmt"
	"os"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	freeProviders "github.com/swadhinbiswas/ghost/internal/config/providers"
)

const nvidiaNimModelsURL = "https://integrate.api.nvidia.com/v1/models"

type nvidiaNimProviderDoc struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	APIEndpoint         string              `json:"api_endpoint"`
	APIKey              string              `json:"api_key"`
	Type                string              `json:"type"`
	DefaultLargeModelID string              `json:"default_large_model_id"`
	DefaultSmallModelID string              `json:"default_small_model_id"`
	Models              []nvidiaNimModelDoc `json:"models"`
}

type nvidiaNimModelDoc struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ContextWindow    int64  `json:"context_window"`
	DefaultMaxTokens int64  `json:"default_max_tokens"`
	CanReason        bool   `json:"can_reason,omitempty"`
}

func nvidiaNimModelsEndpoint() string {
	if value := strings.TrimSpace(os.Getenv("GHOST_NVIDIA_NIM_MODELS_URL")); value != "" {
		return value
	}
	return nvidiaNimModelsURL
}

func fetchNvidiaNimProvider() (catwalk.Provider, error) {
	if os.Getenv("GHOST_NVIDIA_NIM_MODELS_URL") != "" {
		var doc nvidiaNimProviderDoc
		if err := getJSON(nvidiaNimModelsEndpoint(), "", &doc); err != nil {
			return catwalk.Provider{}, fmt.Errorf("nvidia-nim: %w", err)
		}
		return doc.toProvider()
	}
	return fetchNvidiaNimProviderWithKey(os.Getenv("NVIDIA_API_KEY"))
}

func fetchNvidiaNimProviderWithKey(key string) (catwalk.Provider, error) {
	if key == "" {
		return catwalk.Provider{}, fmt.Errorf("nvidia-nim: NVIDIA_API_KEY is required to list models")
	}
	endpoint := strings.TrimSuffix(nvidiaNimModelsEndpoint(), "/models")
	models, err := fetchOpenAIModelCatalog(endpoint, key)
	if err != nil {
		return catwalk.Provider{}, fmt.Errorf("nvidia-nim: %w", err)
	}
	if len(models) == 0 {
		return catwalk.Provider{}, fmt.Errorf("nvidia-nim: catalog has no models")
	}
	provider := freeProviders.NvidiaNimProvider
	provider.Models = models
	provider.DefaultLargeModelID = liveDefault(provider.DefaultLargeModelID, models)
	provider.DefaultSmallModelID = liveDefault(provider.DefaultSmallModelID, models)
	return provider, nil
}

func (d nvidiaNimProviderDoc) toProvider() (catwalk.Provider, error) {
	if d.ID == "" {
		return catwalk.Provider{}, fmt.Errorf("missing provider id")
	}

	models := make([]catwalk.Model, 0, len(d.Models))
	seen := make(map[string]struct{}, len(d.Models))
	for _, item := range d.Models {
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		name := item.Name
		if name == "" {
			name = item.ID
		}
		ctx := item.ContextWindow
		if ctx <= 0 {
			ctx = 128_000
		}
		maxTokens := item.DefaultMaxTokens
		if maxTokens <= 0 {
			maxTokens = 8192
		}
		models = append(models, catwalk.Model{
			ID:               item.ID,
			Name:             name,
			ContextWindow:    ctx,
			DefaultMaxTokens: maxTokens,
			CanReason:        item.CanReason,
		})
	}
	if len(models) == 0 {
		return catwalk.Provider{}, fmt.Errorf("no valid models in document")
	}
	provider := freeProviders.NvidiaNimProvider
	provider.Models = models
	provider.Name = cmpOr(d.Name, provider.Name)
	provider.APIEndpoint = cmpOr(d.APIEndpoint, provider.APIEndpoint)
	provider.APIKey = cmpOr(d.APIKey, provider.APIKey)
	provider.DefaultLargeModelID = liveDefault(d.DefaultLargeModelID, models)
	provider.DefaultSmallModelID = liveDefault(d.DefaultSmallModelID, models)
	return provider, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
