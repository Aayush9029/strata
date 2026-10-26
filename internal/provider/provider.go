package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Aayush9029/strata/internal/catalog"
)

type Translator interface {
	Translate(ctx context.Context, request Request) (Response, error)
}

type Request struct {
	LanguageCode string
	LanguageName string
	Items        []catalog.Item
	Project      ProjectConfig
}

type ProjectConfig struct {
	Catalogs           []string          `json:"catalogs,omitempty"`
	Discover           []string          `json:"discover,omitempty"`
	Languages          []string          `json:"languages,omitempty"`
	LanguageNames      map[string]string `json:"language_names,omitempty"`
	Model              string            `json:"model,omitempty"`
	BatchSize          int               `json:"batch_size,omitempty"`
	Concurrency        int               `json:"concurrency,omitempty"`
	BundleID           string            `json:"bundle_id,omitempty"`
	AppStoreID         string            `json:"app_store_id,omitempty"`
	AppName            string            `json:"app_name,omitempty"`
	Description        string            `json:"description,omitempty"`
	AppContext         string            `json:"app_context,omitempty"`
	ProtectedTerms     []string          `json:"protected_terms,omitempty"`
	Glossary           []string          `json:"glossary,omitempty"`
	StyleGuide         []string          `json:"style_guide,omitempty"`
	SmartContext       bool              `json:"smart_context,omitempty"`
	SmartContextLimit  int               `json:"smart_context_limit,omitempty"`
	SourceRoots        []string          `json:"source_roots,omitempty"`
	AdditionalGuidance string            `json:"additional_guidance,omitempty"`
}

type Response struct {
	Translations map[string]string
	Usage        Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Cost             float64
	HasCost          bool
}

func (u Usage) Add(other Usage) Usage {
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.TotalTokens += other.TotalTokens
	if other.HasCost {
		u.Cost += other.Cost
		u.HasCost = true
	}
	return u
}

func (c ProjectConfig) AllProtectedTerms() []string {
	seen := map[string]bool{}
	terms := []string{}
	for _, term := range append([]string{c.AppName}, c.ProtectedTerms...) {
		term = strings.TrimSpace(term)
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

type OpenRouter struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func NewOpenRouter(apiKey, model string, timeout time.Duration) *OpenRouter {
	return &OpenRouter{
		APIKey: apiKey,
		Model:  model,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (o *OpenRouter) Translate(ctx context.Context, request Request) (Response, error) {
	if o.APIKey == "" {
		return Response{}, fmt.Errorf("OPENROUTER_API_KEY is required")
	}
	rules := promptRules(request.Project)
	contextText := request.Project.AppContext
	if strings.TrimSpace(contextText) == "" {
		contextText = request.Project.Description
	}
	if strings.TrimSpace(contextText) == "" {
		contextText = "The app may contain onboarding, settings, paywalls, product screens, and short user interface labels."
	}

	payload := map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a professional iOS app localizer. Output valid JSON only.",
			},
			{
				"role": "user",
				"content": mustJSON(map[string]any{
					"task":                "Translate iOS app UI strings.",
					"target_language":     request.LanguageName,
					"app_name":            request.Project.AppName,
					"description":         request.Project.Description,
					"app_context":         contextText,
					"protected_terms":     request.Project.AllProtectedTerms(),
					"glossary":            request.Project.Glossary,
					"style_guide":         request.Project.StyleGuide,
					"additional_guidance": request.Project.AdditionalGuidance,
					"rules":               rules,
					"items":               request.Items,
				}),
			},
		},
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+o.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("HTTP-Referer", "https://github.com/Aayush9029/strata")
	httpRequest.Header.Set("X-Title", "strata")

	response, err := o.HTTPClient.Do(httpRequest)
	if err != nil {
		return Response{}, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Response{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, fmt.Errorf("OpenRouter returned %s: %s", response.Status, strings.TrimSpace(string(responseBody)))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int             `json:"prompt_tokens"`
			CompletionTokens int             `json:"completion_tokens"`
			TotalTokens      int             `json:"total_tokens"`
			Cost             json.RawMessage `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return Response{}, err
	}
	if len(completion.Choices) == 0 {
		return Response{}, fmt.Errorf("OpenRouter returned no choices")
	}

	result := map[string]string{}
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &result); err != nil {
		return Response{}, fmt.Errorf("decode model JSON: %w", err)
	}
	return Response{
		Translations: result,
		Usage: Usage{
			PromptTokens:     completion.Usage.PromptTokens,
			CompletionTokens: completion.Usage.CompletionTokens,
			TotalTokens:      completion.Usage.TotalTokens,
			Cost:             parseCost(completion.Usage.Cost),
			HasCost:          len(completion.Usage.Cost) > 0 && string(completion.Usage.Cost) != "null",
		},
	}, nil
}

type Mock struct{}

func (Mock) Translate(ctx context.Context, request Request) (Response, error) {
	_ = ctx
	result := map[string]string{}
	for _, item := range request.Items {
		result[item.ID] = "[" + request.LanguageCode + "] " + item.Source
	}
	return Response{Translations: result}, nil
}

func FromEnvironment(name, model string, timeout time.Duration) (Translator, error) {
	switch name {
	case "", "openrouter":
		return NewOpenRouter(os.Getenv("OPENROUTER_API_KEY"), model, timeout), nil
	case "mock":
		return Mock{}, nil
	default:
		return nil, fmt.Errorf("unknown provider %q", name)
	}
}

func promptRules(project ProjectConfig) []string {
	rules := []string{
		"Return strict JSON only: an object that maps each item id to its translated string.",
		"Keep every placeholder in an item's placeholders list exactly as written: printf specifiers such as %@, %lld, and %1$@, %#@name@ substitution tokens, %arg, and App Shortcuts tokens such as ${applicationName}. Do not translate, add, or remove placeholders.",
		"If the target word order needs a different argument order, use positional specifiers such as %1$@ and %2$@ (keep the type, e.g. %2$lld).",
		"variant_path shows where the string goes. After plural comes a CLDR plural category (zero, one, two, few, many, or other): write the grammatical form of the target language for that category. After device comes a device name such as iphone, ipad, or mac: write the text for that device. After stringSet.values comes the index of one App Shortcuts phrase: write a natural phrase a user would say to Siri, and keep ${applicationName}.",
		"If an item has previous_error, your previous translation (previous_translation) was rejected for that reason. Fix the problem in the new translation.",
		"Use the comment and usage_context to understand each string. usage_context gives the Swift file, type, view, function, and code snippet; role names the UI element (button, navigation title, alert title, and so on); interpolations name the Swift expressions that fill the placeholders, in order.",
		"Keep UI strings concise. Button labels, titles, tabs, and menu items must stay short.",
		"Use the platform's native terminology: Apple's official localized UI terms for the target language, for example \"Einstellungen\" for Settings in German.",
		"Do not add trailing punctuation that the source does not have.",
		"Preserve leading and trailing whitespace, newlines, and escape sequences such as \\n exactly.",
		"Use the typographic quotation marks that are correct for the target language.",
		"Preserve brand names and non-translatable terms exactly.",
		"Do not translate product identifiers, URLs, bundle IDs, route names, or SF Symbol names.",
		"Keep mobile app copy natural and platform appropriate.",
	}
	for _, term := range project.AllProtectedTerms() {
		rules = append(rules, fmt.Sprintf("Do not translate this term: %s.", term))
	}
	return rules
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func parseCost(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		var parsed float64
		if _, err := fmt.Sscanf(text, "%f", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
