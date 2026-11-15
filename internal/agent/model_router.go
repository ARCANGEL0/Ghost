package agent

import (
	"strings"
	"unicode"

	"charm.land/catwalk/pkg/catwalk"
)

func taskKindOf(task string) string {
	text := strings.ToLower(task)
	if hasTaskWord(text, "break into", "subtasks", "make a plan", "outline phases") {
		return "planning"
	}
	if hasTaskWord(text, "implement", "fix", "write", "create", "change", "update", "add", "refactor", "debug", "test", "modify", "build") {
		return "implementation"
	}
	if hasTaskWord(text, "find", "inspect", "analyze", "compare", "trace", "investigate", "explain", "summarize", "search", "review", "document") {
		return "research"
	}
	return "general"
}

func hasTaskWord(text string, words ...string) bool {
	seen := make(map[string]struct{})
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		seen[word] = struct{}{}
	}
	for _, word := range words {
		if strings.Contains(word, " ") && strings.Contains(text, word) {
			return true
		}
		if _, ok := seen[word]; ok {
			return true
		}
		if strings.HasSuffix(word, "e") {
			if _, ok := seen[word+"s"]; ok {
				return true
			}
		}
		if _, ok := seen[strings.TrimSuffix(word, "e")+"ed"]; ok {
			return true
		}
	}
	return false
}

func (c *coordinator) routeTaskModel(task string, large, small Model, needsTools bool) Model {
	kind := taskKindOf(task)
	largeFits := modelFitsTask(large, task)
	smallFits := modelFitsTask(small, task)
	if !largeFits && smallFits && (!needsTools || c.modelSupportsTools(small)) {
		return small
	}
	if kind == "implementation" {
		return large
	}
	if !smallFits || (needsTools && !c.modelSupportsTools(small)) {
		return large
	}
	if !largeFits {
		return small
	}
	if kind == "planning" {
		return small
	}

	tokens := estimateTaskTokens(task)
	smallCost := estimateModelCost(small, tokens)
	largeCost := estimateModelCost(large, tokens)
	smallLatency := c.runs.averageLatency(small.ModelCfg.Provider + "/" + small.ModelCfg.Model)
	largeLatency := c.runs.averageLatency(large.ModelCfg.Provider + "/" + large.ModelCfg.Model)
	smallSlower := smallLatency > 0 && largeLatency > 0 && smallLatency > largeLatency*3/2
	if smallCost <= largeCost && !smallSlower {
		return small
	}
	if smallLatency > 0 && largeLatency > 0 && smallLatency < largeLatency*4/5 && smallCost <= largeCost*1.2 {
		return small
	}
	return large
}

func (c *coordinator) modelSupportsTools(model Model) bool {
	if model.Model == nil {
		return false
	}
	provider, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return false
	}
	switch provider.Type {
	case catwalk.TypeOpenAI, catwalk.TypeOpenAICompat, catwalk.TypeOpenRouter,
		catwalk.TypeAnthropic, catwalk.TypeGoogle, catwalk.TypeAzure,
		catwalk.TypeBedrock, catwalk.TypeVertexAI, catwalk.TypeVercel:
		return !isNonChatModel(model.CatwalkCfg.ID)
	default:
		return false
	}
}

func isNonChatModel(modelID string) bool {
	modelID = strings.ToLower(modelID)
	for _, kind := range []string{"embedding", "rerank", "moderation", "transcription", "tts", "whisper"} {
		if strings.Contains(modelID, kind) {
			return true
		}
	}
	return false
}

func modelFitsTask(model Model, task string) bool {
	window := model.CatwalkCfg.ContextWindow
	if model.Model == nil || window <= 0 {
		return false
	}
	tokens := estimateTaskTokens(task)
	output := max(int64(256), min(int64(2048), tokens/2))
	return tokens+output+4096 <= window
}

func estimateTaskTokens(task string) int64 {
	return max(int64(1), int64(len([]rune(task))/4)+4096)
}

func estimateModelCost(model Model, tokens int64) float64 {
	input := float64(tokens)
	output := float64(max(int64(256), min(int64(2048), tokens/2)))
	return (model.CatwalkCfg.CostPer1MIn*input + model.CatwalkCfg.CostPer1MOut*output) / 1_000_000
}
