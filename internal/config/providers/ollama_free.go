// ollama_free.go: Ollama local provider (truly free, runs on user's GPU).
//
// Base URL: http://localhost:11434/v1
// Auth: none
//
// User runs `ollama serve` then `ollama pull <model>` once.
package providers

import "charm.land/catwalk/pkg/catwalk"

var OllamaLocalProvider = catwalk.Provider{
	ID:          "ollama-local",
	Name:        "Ollama (local, free forever)",
	APIEndpoint: "http://localhost:11434/v1",
	APIKey:      "ollama", // required by some clients but ignored by Ollama
	Type:        catwalk.TypeOpenAICompat,
}
