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
		AmdRadeonProvider,
		AionProvider,
		HuggingFaceProvider,
		LLM7Provider,
		TyphoonProvider,
		NousProvider,
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
	case "opencode", "opencode-zen", "openrouter", "kilo", "oi-vscode", "amd-radeon", "nous-portal":
		return TierFree
	case "ollama-local":
		return TierLocal
	case "googleai", "groq", "cloudflare", "cohere", "nvidia-nim", "aion", "llm7", "typhoon":
		return TierQuota
	case "cerebras", "huggingface":
		return TierCredits
	default:
		return ""
	}
}
