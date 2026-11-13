package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// Catalog adapters normalize provider model lists for Ghost.
type modelCatalog struct {
	Data []struct {
		ID                  string          `json:"id"`
		Name                string          `json:"name"`
		ContextLength       int64           `json:"context_length"`
		ContextWindow       int64           `json:"context_window"`
		MaxCompletionTokens int64           `json:"max_completion_tokens"`
		Pricing             json.RawMessage `json:"pricing"`
		Architecture        struct {
			Modality         string   `json:"modality"`
			InputModalities  []string `json:"input_modalities"`
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
	} `json:"data"`
}

type llm7Catalog struct {
	Data []struct {
		ID             string `json:"id"`
		ModelType      string `json:"model_type"`
		UsageBasedOnly *bool  `json:"usage_based_only"`
		Modalities     struct {
			Output []string `json:"output"`
		} `json:"modalities"`
		ContextWindow struct {
			Tokens int64 `json:"tokens"`
		} `json:"context_window"`
		Pricing json.RawMessage `json:"pricing"`
	} `json:"data"`
}

type typhoonCatalog []struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type googleCatalog struct {
	Models []struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		InputTokenLimit            int64    `json:"inputTokenLimit"`
		OutputTokenLimit           int64    `json:"outputTokenLimit"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

type cohereCatalog struct {
	Models []struct {
		Name         string   `json:"name"`
		Endpoints    []string `json:"endpoints"`
		ContextWidth int64    `json:"context_length"`
	} `json:"models"`
}

type cloudflareCatalog struct {
	Result     []cloudflareCatalogItem `json:"result"`
	ResultInfo struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type cloudflareCatalogItem struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Task        json.RawMessage      `json:"task"`
	Properties  []cloudflareProperty `json:"properties"`
}

type cloudflareProperty struct {
	PropertyID string `json:"property_id"`
	Value      any    `json:"value"`
}

type amdCatalog struct {
	Data []struct {
		ID            string          `json:"id"`
		Name          string          `json:"name"`
		ContextLength int64           `json:"context_length"`
		Free          bool            `json:"free"`
		Pricing       json.RawMessage `json:"pricing"`
		Architecture  struct {
			InputModalities  []string `json:"input_modalities"`
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
		SupportedParameters []string `json:"supported_parameters"`
		Providers           []struct {
			Vision    bool `json:"vision"`
			Reasoning bool `json:"reasoning"`
		} `json:"providers"`
	} `json:"data"`
}

func fetchOpenAIModelCatalog(endpoint, key string) ([]catwalk.Model, error) {
	return fetchOpenAIModels(endpoint, key, false)
}

func fetchFreeOpenAIModelCatalog(endpoint, key string) ([]catwalk.Model, error) {
	return fetchOpenAIModels(endpoint, key, true)
}

func fetchOpenAIModels(endpoint, key string, freeOnly bool) ([]catwalk.Model, error) {
	modelsURL := strings.TrimRight(endpoint, "/") + "/models"
	var parsed modelCatalog
	if err := getJSON(modelsURL, key, &parsed); err != nil {
		return nil, err
	}

	models := make([]catwalk.Model, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID == "" || isNonChatModel(item.ID) || !isChatOutput(item.Architecture.OutputModalities, item.Architecture.Modality) {
			continue
		}
		if freeOnly && !isZeroPrice(item.Pricing) {
			continue
		}
		ctx := item.ContextLength
		if ctx <= 0 {
			ctx = item.ContextWindow
		}
		if ctx <= 0 {
			ctx = 128_000
		}
		maxTokens := item.MaxCompletionTokens
		if maxTokens <= 0 {
			maxTokens = 8192
		}
		name := item.Name
		if name == "" {
			name = displayNameFromID(item.ID)
		}
		model := catwalk.Model{
			ID:               item.ID,
			Name:             name,
			ContextWindow:    ctx,
			DefaultMaxTokens: maxTokens,
		}
		model.CostPer1MIn, model.CostPer1MOut = readModelPrice(item.Pricing)
		for _, modality := range item.Architecture.InputModalities {
			if modality == "image" {
				model.SupportsImages = true
			}
		}
		models = append(models, model)
	}
	return models, nil
}

func fetchLLM7ModelCatalog(endpoint, key string) ([]catwalk.Model, error) {
	var parsed llm7Catalog
	if err := getJSON(strings.TrimRight(endpoint, "/")+"/models", key, &parsed); err != nil {
		return nil, err
	}
	models := make([]catwalk.Model, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID == "" || item.ModelType != "chat" || item.UsageBasedOnly == nil || *item.UsageBasedOnly || !hasMethod(item.Modalities.Output, "text") {
			continue
		}
		ctx := item.ContextWindow.Tokens
		if ctx <= 0 {
			ctx = 128_000
		}
		model := catwalk.Model{
			ID:               item.ID,
			Name:             displayNameFromID(item.ID),
			ContextWindow:    ctx,
			DefaultMaxTokens: 8192,
		}
		model.CostPer1MIn, model.CostPer1MOut = readModelPrice(item.Pricing)
		models = append(models, model)
	}
	return models, nil
}

func fetchTyphoonModelCatalog(endpoint, key string) ([]catwalk.Model, error) {
	var parsed typhoonCatalog
	if err := getJSON(strings.TrimRight(endpoint, "/")+"/models", key, &parsed); err != nil {
		return nil, err
	}
	models := make([]catwalk.Model, 0, len(parsed))
	for _, item := range parsed {
		id := strings.ToLower(item.ID)
		if !strings.HasPrefix(id, "typhoon-v") || !strings.Contains(id, "instruct") {
			continue
		}
		name := item.DisplayName
		if name == "" {
			name = displayNameFromID(item.ID)
		}
		models = append(models, catwalk.Model{
			ID:               item.ID,
			Name:             name,
			ContextWindow:    128_000,
			DefaultMaxTokens: 8192,
		})
	}
	return models, nil
}

func fetchGoogleModelCatalog(endpoint, key string) ([]catwalk.Model, error) {
	models, err := fetchOpenAIModelCatalog(endpoint, key)
	if err == nil {
		return filterGoogleModels(models), nil
	}

	var google googleCatalog
	if googleErr := getJSON("https://generativelanguage.googleapis.com/v1beta/models?key="+url.QueryEscape(key), "", &google); googleErr != nil {
		return nil, err
	}
	for _, item := range google.Models {
		id := strings.TrimPrefix(item.Name, "models/")
		if isNonChatModel(id) || !isGoogleFreeModel(id) || !hasMethod(item.SupportedGenerationMethods, "generateContent") {
			continue
		}
		name := item.DisplayName
		if name == "" {
			name = displayNameFromID(id)
		}
		models = append(models, catwalk.Model{
			ID:               id,
			Name:             name,
			ContextWindow:    item.InputTokenLimit,
			DefaultMaxTokens: item.OutputTokenLimit,
			SupportsImages:   true,
		})
	}
	return models, nil
}

func fetchCohereModelCatalog(key string) ([]catwalk.Model, error) {
	var parsed cohereCatalog
	if err := getJSON("https://api.cohere.com/v1/models?endpoint=chat&page_size=1000", key, &parsed); err != nil {
		return nil, err
	}
	models := make([]catwalk.Model, 0, len(parsed.Models))
	for _, item := range parsed.Models {
		if item.Name == "" || !hasMethod(item.Endpoints, "chat") {
			continue
		}
		ctx := item.ContextWidth
		if ctx <= 0 {
			ctx = 128_000
		}
		models = append(models, catwalk.Model{
			ID:               item.Name,
			Name:             displayNameFromID(item.Name),
			ContextWindow:    ctx,
			DefaultMaxTokens: 8192,
		})
	}
	return models, nil
}

func fetchAmdModelCatalog(endpoint, key string) ([]catwalk.Model, error) {
	var parsed amdCatalog
	if err := getJSON(strings.TrimRight(endpoint, "/")+"/models", key, &parsed); err != nil {
		return nil, err
	}
	models := make([]catwalk.Model, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID == "" || isNonChatModel(item.ID) || !item.Free || !hasMethod(item.Architecture.OutputModalities, "text") || !hasMethod(item.SupportedParameters, "max_tokens") {
			continue
		}
		ctx := item.ContextLength
		if ctx <= 0 {
			ctx = 128_000
		}
		name := item.Name
		if name == "" {
			name = displayNameFromID(item.ID)
		}
		model := catwalk.Model{
			ID:               item.ID,
			Name:             name,
			ContextWindow:    ctx,
			DefaultMaxTokens: 8192,
		}
		model.CostPer1MIn, model.CostPer1MOut = readModelPrice(item.Pricing)
		for _, modality := range item.Architecture.InputModalities {
			if modality == "image" {
				model.SupportsImages = true
			}
		}
		for _, provider := range item.Providers {
			model.CanReason = model.CanReason || provider.Reasoning
			model.SupportsImages = model.SupportsImages || provider.Vision
		}
		models = append(models, model)
	}
	return models, nil
}

func fetchCloudflareModelCatalog(account, key string) ([]catwalk.Model, error) {
	if account == "" || key == "" {
		return nil, fmt.Errorf("cloudflare: account ID and API token are required")
	}
	base := "https://api.cloudflare.com/client/v4/accounts/" + url.PathEscape(account) + "/ai/models/search"
	var items []cloudflareCatalogItem
	for page := 1; page <= 20; page++ {
		query := url.Values{"task": {"text-generation"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		var parsed cloudflareCatalog
		if err := getJSON(base+"?"+query.Encode(), key, &parsed); err != nil {
			return nil, err
		}
		items = append(items, parsed.Result...)
		if parsed.ResultInfo.TotalPages == 0 || page >= parsed.ResultInfo.TotalPages {
			break
		}
	}
	models := make([]catwalk.Model, 0, len(items))
	for _, item := range items {
		id := item.ID
		if id == "" {
			id = item.Name
		}
		if id == "" || isCloudflarePaidModel(id) {
			continue
		}
		if !isTextGenerationTask(item.Task) {
			continue
		}
		name := item.Name
		if name == "" {
			name = displayNameFromID(id)
		}
		ctx, maxTokens := cloudflareLimits(item.Properties)
		models = append(models, catwalk.Model{
			ID:               id,
			Name:             name,
			ContextWindow:    ctx,
			DefaultMaxTokens: maxTokens,
			CanReason:        strings.Contains(strings.ToLower(id), "glm-") || strings.Contains(strings.ToLower(id), "nemotron"),
			SupportsImages:   strings.Contains(strings.ToLower(id), "vision") || strings.Contains(strings.ToLower(id), "vl-"),
		})
	}
	return models, nil
}

func cloudflareLimits(properties []cloudflareProperty) (int64, int64) {
	ctx, maxTokens := int64(32_000), int64(8192)
	for _, item := range properties {
		value, ok := numberValue(item.Value)
		if !ok {
			continue
		}
		switch strings.ToLower(item.PropertyID) {
		case "max_total_tokens", "context_length", "max_context_length":
			ctx = value
		case "max_output_tokens", "max_completion_tokens":
			maxTokens = value
		}
	}
	return ctx, maxTokens
}

func numberValue(value any) (int64, bool) {
	switch value := value.(type) {
	case float64:
		return int64(value), value > 0
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

func isTextGenerationTask(raw json.RawMessage) bool {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		name = strings.ToLower(name)
		return strings.Contains(name, "text generation") || strings.Contains(name, "text-generation")
	}
	var task struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &task) != nil {
		return false
	}
	name = strings.ToLower(task.Name + " " + task.ID)
	return strings.Contains(name, "text generation") || strings.Contains(name, "text-generation")
}

func getJSON(endpoint, key string, dst any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if key != "" && key != "no-key-needed" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("catalog request returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return err
	}
	return nil
}

func filterGoogleModels(models []catwalk.Model) []catwalk.Model {
	filtered := make([]catwalk.Model, 0, len(models))
	for _, model := range models {
		if isGoogleFreeModel(model.ID) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func isGoogleFreeModel(id string) bool {
	id = strings.TrimPrefix(id, "models/")
	if id == "gemini-3-flash-preview" || id == "gemini-2.5-pro" {
		return true
	}
	if strings.Contains(id, "pro") || strings.Contains(id, "preview") || strings.Contains(id, "image") || strings.Contains(id, "tts") || strings.Contains(id, "live") {
		return false
	}
	return strings.HasPrefix(id, "gemini-3.5-flash") ||
		strings.HasPrefix(id, "gemini-3.6-flash") ||
		strings.HasPrefix(id, "gemini-3.7-flash") ||
		strings.HasPrefix(id, "gemini-3.8-flash") ||
		strings.HasPrefix(id, "gemini-3.1-flash-lite") ||
		strings.HasPrefix(id, "gemini-2.5-flash") ||
		strings.HasPrefix(id, "gemini-2.5-flash-lite") ||
		strings.HasPrefix(id, "gemma-4")
}

func hasMethod(methods []string, want string) bool {
	for _, method := range methods {
		if strings.EqualFold(method, want) {
			return true
		}
	}
	return false
}

func isChatOutput(modalities []string, modality string) bool {
	if len(modalities) > 0 {
		return hasMethod(modalities, "text")
	}
	if parts := strings.SplitN(modality, "->", 2); len(parts) == 2 {
		return strings.Contains(strings.ToLower(parts[1]), "text")
	}
	return true
}

func isNonChatModel(id string) bool {
	id = strings.ToLower(id)
	for _, marker := range []string{"embedding", "embed-", "rerank", "whisper", "text-to-speech", "stable-diffusion"} {
		if strings.Contains(id, marker) {
			return true
		}
	}
	return strings.HasPrefix(id, "tts-")
}

func isCloudflarePaidModel(id string) bool {
	id = strings.ToLower(id)
	return strings.Contains(id, "kimi-k2") || strings.Contains(id, "glm-5.") || strings.Contains(id, "deepseek-v4-")
}

func readModelPrice(raw json.RawMessage) (float64, float64) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, 0
	}
	var prices map[string]json.RawMessage
	if err := json.Unmarshal(raw, &prices); err != nil {
		return 0, 0
	}
	input := prices["input"]
	output := prices["output"]
	if len(input) == 0 {
		input = prices["prompt"]
	}
	if len(output) == 0 {
		output = prices["completion"]
	}
	return priceValue(input), priceValue(output)
}

func isZeroPrice(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var prices map[string]json.RawMessage
	if err := json.Unmarshal(raw, &prices); err != nil {
		return false
	}
	input, hasInput := prices["input"]
	output, hasOutput := prices["output"]
	if !hasInput {
		input, hasInput = prices["prompt"]
	}
	if !hasOutput {
		output, hasOutput = prices["completion"]
	}
	return hasInput && hasOutput && zeroPrice(input) && zeroPrice(output)
}

// zeroPrice accepts only an explicit numeric zero.
func zeroPrice(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return number == 0
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return false
	}
	number, err := strconv.ParseFloat(text, 64)
	return err == nil && number == 0
}

func priceValue(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return value * 1_000_000
}
