package models

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// AI providers accepted in AISettingsRequest.Provider.
const (
	AIProviderOpenAI    = "openai" // any OpenAI-compatible endpoint
	AIProviderAnthropic = "anthropic"
)

// AISettingsRequest is the body of PUT /api/ai/settings. A nil APIKey keeps
// the stored key unless Provider or BaseURL changes; an empty one clears it.
type AISettingsRequest struct {
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	VisionModel string  `json:"vision_model"`
	APIKey      *string `json:"api_key"`
}

func (r *AISettingsRequest) Validate() error {
	r.Provider = strings.TrimSpace(r.Provider)
	r.BaseURL = strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	r.Model = strings.TrimSpace(r.Model)
	r.VisionModel = strings.TrimSpace(r.VisionModel)

	if r.Provider != AIProviderOpenAI && r.Provider != AIProviderAnthropic {
		return errors.New("provider must be 'openai' or 'anthropic'")
	}
	if err := validateBaseURL(r.BaseURL); err != nil {
		return err
	}
	if err := validateModelName("model", r.Model, true); err != nil {
		return err
	}
	if err := validateModelName("vision_model", r.VisionModel, false); err != nil {
		return err
	}
	if r.APIKey != nil {
		key := strings.TrimSpace(*r.APIKey)
		r.APIKey = &key
		if len(key) > 500 || strings.ContainsAny(key, " \t\r\n") {
			return errors.New("api_key must be at most 500 characters without whitespace")
		}
	}
	return nil
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > 300 {
		return errors.New("base_url must be 300 characters or fewer")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("base_url must be an http(s) URL without credentials")
	}
	return nil
}

func validateModelName(field, name string, required bool) error {
	if name == "" {
		if required {
			return errors.New(field + " is required")
		}
		return nil
	}
	if utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, " \t\r\n<>") {
		return errors.New(field + " must be at most 100 characters without whitespace")
	}
	return nil
}

// Chat roles accepted in ChatMessage.Role.
const (
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
)

const (
	chatMaxMessages = 20
	chatMaxContent  = 4000
)

// ChatMessage is one turn of an assistant conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the body of POST /api/ai/chat.
type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
}

func (r *ChatRequest) Validate() error {
	if len(r.Messages) == 0 || len(r.Messages) > chatMaxMessages {
		return errors.New("messages must have 1-20 entries")
	}
	for i := range r.Messages {
		m := &r.Messages[i]
		m.Content = strings.TrimSpace(m.Content)
		if m.Role != ChatRoleUser && m.Role != ChatRoleAssistant {
			return errors.New("message role must be 'user' or 'assistant'")
		}
		if m.Content == "" || utf8.RuneCountInString(m.Content) > chatMaxContent {
			return errors.New("message content must be 1-4000 characters")
		}
	}
	if r.Messages[len(r.Messages)-1].Role != ChatRoleUser {
		return errors.New("last message must be from the user")
	}
	return nil
}
