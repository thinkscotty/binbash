// Package ai implements a minimal client for OpenAI-compatible chat
// completions endpoints, used to suggest search keywords for items.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxCompletionTokens caps the model's reply length. It's set generously
// because reasoning models (e.g. Qwen3, DeepSeek-R1 style) spend most of
// their output "thinking" -- often in a separate reasoning_content field --
// before emitting the actual answer in content. Too low a cap (the previous
// 200) truncated them mid-thought, so content came back empty and no tags
// were parsed. 2048 gives a reasoning model room to finish and still return
// its tags; if a model somehow blows past even this, the handler treats the
// empty result as "retry later" rather than marking the item done.
const maxCompletionTokens = 2048

// Client talks to an OpenAI-compatible chat completions endpoint at
// baseURL + "/chat/completions". Any provider that speaks this shape
// (OpenAI, Chutes.ai, or a compatibility layer in front of Gemini/Claude)
// works without provider-specific code.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ItemContext carries the details of an item (and its bin) used to build
// the tagging prompt. Only Name is required; the rest are included in the
// prompt only when non-empty.
type ItemContext struct {
	Name             string
	Description      string
	ExistingKeywords string
	BinName          string
	BinCategory      string
	BinDescription   string
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// TagItem asks the configured endpoint for up to tagCount keyword tags for
// item, with breadth ("narrow", "moderate", or "broad") shaping how closely
// related the suggestions should be. Returns nil, nil without making a
// request when tagCount is 0.
func (c *Client) TagItem(ctx context.Context, item ItemContext, tagCount int, breadth string) ([]string, error) {
	if tagCount == 0 {
		return nil, nil
	}

	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt(tagCount, breadth)},
			{Role: "user", Content: userPrompt(item)},
		},
		Temperature: 0.3,
		MaxTokens:   maxCompletionTokens,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var parsed chatResponse
	if jsonErr := json.Unmarshal(respBody, &parsed); jsonErr != nil {
		return nil, fmt.Errorf("AI provider returned status %d with an unparseable body", resp.StatusCode)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("AI provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI provider returned status %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("AI provider returned no choices")
	}

	return parseTags(parsed.Choices[0].Message.Content, tagCount), nil
}

// parseTags splits a model reply into individual tags, tolerating commas or
// newlines as separators and stray quoting/whitespace around each tag.
func parseTags(content string, max int) []string {
	fields := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n'
	})

	tags := make([]string, 0, len(fields))
	for _, f := range fields {
		t := strings.Trim(strings.TrimSpace(f), `"'`)
		if t == "" {
			continue
		}
		tags = append(tags, t)
		if len(tags) == max {
			break
		}
	}
	return tags
}

func systemPrompt(tagCount int, breadth string) string {
	// example pairs each breadth with a one-shot demonstration for the same
	// input ("London Lego Set"), so the model sees not just an instruction but
	// a worked example of expanding *beyond* the item's own name at the right
	// breadth. Keeping the example input constant across breadths makes the
	// difference between the levels legible: narrow stays on alternate names,
	// broad reaches out to themes and use cases.
	var guidance, example string
	switch breadth {
	case "narrow":
		guidance = "Suggest only close synonyms and alternate names for the item — nothing broader."
		example = "building blocks, brick set, construction toy"
	case "broad":
		guidance = "Suggest synonyms, alternate names, general categories, and common use cases or related items."
		example = "toys, bricks, travel, souvenir, display piece"
	default: // "moderate"
		guidance = "Suggest close synonyms, alternate names, and the item's general category."
		example = "toys, bricks, travel"
	}
	return fmt.Sprintf(
		"You are a tagging assistant for a home inventory app. Given an item's details, suggest up to %d short search keywords/tags that help someone find this item later by typing a RELATED term. %s "+
			"Do not simply repeat words that already appear in the item's name or its existing keywords — those are already searchable. Instead suggest terms the item could ALSO be found by: its category, material, purpose, or theme. "+
			"For example, for an item named \"London Lego Set\", good tags would be: %s. "+
			"Respond with ONLY a comma-separated list of tags and nothing else — no explanation, no numbering. If you have no useful tags to add, respond with an empty string.",
		tagCount, guidance, example,
	)
}

func userPrompt(item ItemContext) string {
	lines := []string{"Item: " + item.Name}
	if item.Description != "" {
		lines = append(lines, "Description: "+item.Description)
	}
	if item.ExistingKeywords != "" {
		lines = append(lines, "Existing keywords: "+item.ExistingKeywords)
	}
	if item.BinName != "" {
		lines = append(lines, "Stored in bin: "+item.BinName)
	}
	if item.BinCategory != "" {
		lines = append(lines, "Bin category: "+item.BinCategory)
	}
	if item.BinDescription != "" {
		lines = append(lines, "Bin description: "+item.BinDescription)
	}
	return strings.Join(lines, "\n")
}
