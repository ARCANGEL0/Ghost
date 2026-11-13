package providers

import "charm.land/catwalk/pkg/catwalk"

func AllFreeProviders() []catwalk.Provider {
	return []catwalk.Provider{
		OpenRouterFreeProvider,
		KiloProvider,
		OpenCodeZenProvider,
		OIVSCodeProvider,
		GroqProvider,
		CerebrasProvider,
		GoogleAIProvider,
		CloudflareProvider,
		CohereProvider,
		NvidiaNimProvider,
		AionProvider,
		HuggingFaceProvider,
		OllamaLocalProvider,
	}
}

func AllProviders() []catwalk.Provider {
	return AllFreeProviders()
}

type Tier string

const (
	TierFree    Tier = "free"
	TierQuota   Tier = "quota"
	TierCredits Tier = "credits"
	TierLocal   Tier = "local"
)

func ProviderTier(id string) Tier {
	switch id {
	case "opencode", "opencode-zen", "openrouter", "kilo", "oi-vscode":
		return TierFree
	case "ollama-local":
		return TierLocal
	case "googleai", "groq", "cerebras", "cloudflare", "cohere", "nvidia-nim":
		return TierQuota
	case "aion", "huggingface":
		return TierCredits
	default:
		return ""
	}
}

func AllFreeModelIDs() map[string]map[string]struct{} {
	return map[string]map[string]struct{}{
		"opencode-zen": OpenCodeZenFreeModelIDs(),
	}
}
