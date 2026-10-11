package assistant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/vinegod/medvault/backend/internal/models"
)

// Reply is the assistant's answer to a chat.
type Reply struct {
	Text        string   `json:"text"`
	Warning     string   `json:"warning,omitempty"`
	MedicineIDs []string `json:"medicine_ids"` // inventory items named in Text
}

const chatMaxTurns = 5

// Chat answers the conversation using read-only tools over the household's
// inventory and the drug DB. msgs must already be validated.
func (a *Assistant) Chat(ctx context.Context, householdID string, msgs []models.ChatMessage) (Reply, error) {
	cfg, e, err := a.prepare(ctx)
	if err != nil {
		return Reply{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.opts.Timeout)
	defer cancel()
	ctx, seen := withHousehold(ctx, householdID)

	history := make([]*ai.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == models.ChatRoleAssistant {
			history = append(history, ai.NewModelTextMessage(m.Content))
			continue
		}
		history = append(history, ai.NewUserTextMessage(m.Content))
	}

	resp, err := genkit.Generate(ctx, e.g,
		ai.WithModelName(modelName(&cfg, cfg.Model)),
		ai.WithSystem(fmt.Sprintf(chatPrompt, time.Now().UTC().Format(time.DateOnly))),
		ai.WithMessages(history...),
		ai.WithTools(e.tools...),
		ai.WithMaxTurns(chatMaxTurns),
	)
	if err != nil {
		if errors.Is(err, ai.ErrMaxTurnsExceeded) {
			return Reply{}, fmt.Errorf("chat: %w", ErrUpstream)
		}
		return Reply{}, upstreamError(ctx, "chat", err)
	}

	text, warning := splitWarning(resp.Text())
	if text == "" && warning == "" {
		return Reply{}, ErrUpstream
	}
	return Reply{Text: text, Warning: warning, MedicineIDs: seen.mentionedIn(text)}, nil
}

// splitWarning separates a leading "WARNING:" line from the answer.
func splitWarning(s string) (text, warning string) {
	s = strings.TrimSpace(s)
	first, rest, _ := strings.Cut(s, "\n")
	label, reason, ok := strings.Cut(strings.TrimSpace(first), ":")
	if !ok || !strings.EqualFold(strings.Trim(label, "*_ "), "warning") {
		return s, ""
	}
	return strings.TrimSpace(rest), strings.TrimSpace(strings.Trim(reason, "*_ "))
}

// mentionedIn returns the ids of seen items whose name occurs in text.
func (s *seenItems) mentionedIn(text string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	lower := strings.ToLower(text)
	ids := []string{}
	for id, name := range s.names {
		if name != "" && strings.Contains(lower, strings.ToLower(name)) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
