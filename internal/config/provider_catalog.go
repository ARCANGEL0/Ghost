package config

import (
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
	freeProviders "github.com/swadhinbiswas/ghost/internal/config/providers"
)

// AuthMethod is a way to authenticate to a provider. A provider can offer more
// than one method; the order returned by ProviderAuthMethods is the order the
// UI offers them.
type AuthMethod string

const (
	// AuthAPIKey stores a static key in the Ghost config.
	AuthAPIKey AuthMethod = "api_key"
	// AuthOAuth runs a subscription sign-in and stores the resulting token.
	AuthOAuth AuthMethod = "oauth"
	// AuthCLI delegates sign-in to the provider's own command line tool.
	AuthCLI AuthMethod = "cli"
	// AuthNone means the provider needs no credentials, for example a local
	// server or a free endpoint.
	AuthNone AuthMethod = "none"
)

// Label returns a short name for the method, suitable for a list row.
func (m AuthMethod) Label() string {
	switch m {
	case AuthAPIKey:
		return "API key"
	case AuthOAuth:
		return "Subscription sign-in"
	case AuthCLI:
		return "CLI sign-in"
	case AuthNone:
		return "No sign-in needed"
	default:
		return string(m)
	}
}

// Hint returns one line of context shown under the method name.
func (m AuthMethod) Hint() string {
	switch m {
	case AuthAPIKey:
		return "Paste a key from the provider dashboard"
	case AuthOAuth:
		return "Use an existing plan or subscription"
	case AuthCLI:
		return "Open the provider's own login command"
	case AuthNone:
		return "Local or free, no credentials required"
	default:
		return ""
	}
}

// ProviderCatalog returns every provider Ghost can configure. It combines the
// curated free list, the live catalog, the embedded Catwalk catalog, and any
// provider already present in the user config. Curated free gateways come first.
func ProviderCatalog(cfg *Config) []catwalk.Provider {
	seen := make(map[string]bool)
	catalog := make([]catwalk.Provider, 0, 48)
	priority := make(map[string]int)
	for i, provider := range freeProviders.AllFreeProviders() {
		priority[string(provider.ID)] = i
	}

	add := func(p catwalk.Provider) {
		id := string(p.ID)
		if id == "" || seen[id] || !configurableProvider(id) {
			return
		}
		seen[id] = true
		catalog = append(catalog, p)
	}

	if providers, err := Providers(cfg); err == nil {
		for _, p := range providers {
			add(p)
		}
	}

	for _, p := range embedded.GetAll() {
		add(p)
	}

	if cfg != nil && cfg.Providers != nil {
		for id, providerConfig := range cfg.Providers.Seq2() {
			if seen[id] {
				continue
			}
			add(providerConfig.ToProvider())
		}
	}

	slices.SortFunc(catalog, func(a, b catwalk.Provider) int {
		aRank, aFree := priority[string(a.ID)]
		bRank, bFree := priority[string(b.ID)]
		if aFree && bFree {
			return aRank - bRank
		}
		if aFree {
			return -1
		}
		if bFree {
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return catalog
}

// ProviderTierLabel returns the short free access note shown in provider lists.
func ProviderTierLabel(id string) string {
	switch freeProviders.ProviderTier(id) {
	case freeProviders.TierFree:
		return "free models"
	case freeProviders.TierQuota:
		return "free quota"
	case freeProviders.TierCredits:
		return "free credits"
	case freeProviders.TierLocal:
		return "local"
	default:
		return ""
	}
}

// configurableProvider filters out providers whose setup needs more than an
// API key or a sign-in, such as cloud projects and IAM roles.
func configurableProvider(id string) bool {
	switch id {
	case "azure", "vertexai", "bedrock":
		return false
	default:
		return true
	}
}

// withEmbeddedProviders appends the embedded Catwalk catalog to a provider
// list. It keeps the curated entries already present, so Ghost specific
// endpoints and models win over the generic catalog.
func withEmbeddedProviders(known []catwalk.Provider) []catwalk.Provider {
	seen := make(map[string]bool, len(known))
	for _, p := range known {
		seen[string(p.ID)] = true
	}
	for _, p := range embedded.GetAll() {
		if seen[string(p.ID)] || !configurableProvider(string(p.ID)) {
			continue
		}
		seen[string(p.ID)] = true
		known = append(known, p)
	}
	return known
}

// ProviderAuthMethods returns the sign-in methods a provider supports. Most
// providers accept an API key. A handful have a real subscription flow in
// Ghost, and local or CLI-backed providers need no stored credentials.
func ProviderAuthMethods(id string) []AuthMethod {
	switch id {
	case string(catwalk.InferenceProviderAnthropic):
		return []AuthMethod{AuthAPIKey}
	case string(catwalk.InferenceProviderCopilot):
		return []AuthMethod{AuthOAuth}
	case "hyper":
		return []AuthMethod{AuthOAuth}
	case "codex":
		return []AuthMethod{AuthCLI}
	case "claude-code":
		return []AuthMethod{AuthCLI}
	case "auto-free", "ollama-local", "oi-vscode", "kilo":
		return []AuthMethod{AuthNone}
	default:
		return []AuthMethod{AuthAPIKey}
	}
}

// LookupProvider finds a provider definition by ID. The curated free list wins
// over the embedded catalog because it carries Ghost specific models and
// endpoints.
func LookupProvider(id string) (catwalk.Provider, bool) {
	if id == string(freeProviders.ClaudeCodeProvider.ID) {
		return freeProviders.ClaudeCodeProvider, true
	}
	for _, p := range freeProviders.AllFreeProviders() {
		if string(p.ID) == id {
			return p, true
		}
	}
	for _, p := range embedded.GetAll() {
		if string(p.ID) == id {
			return p, true
		}
	}
	return catwalk.Provider{}, false
}
