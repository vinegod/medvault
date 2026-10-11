package assistant

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vinegod/medvault/backend/internal/models"
)

// Settings is the effective model config. The API key is never included.
type Settings struct {
	Provider    string `json:"provider"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	VisionModel string `json:"vision_model"`
	APIKeySet   bool   `json:"api_key_set"`
	Source      string `json:"source"` // web | env | none
}

// Settings returns the effective model config.
func (a *Assistant) Settings(ctx context.Context) (Settings, error) {
	row, ok, err := a.loadSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	if ok {
		return Settings{
			Provider: row.provider, BaseURL: row.baseURL, Model: row.model, VisionModel: row.visionModel,
			APIKeySet: row.apiKeyEnc != nil, Source: sourceWeb,
		}, nil
	}
	d := a.opts.Default
	if d.Model == "" {
		return Settings{Source: sourceNone}, nil
	}
	return Settings{
		Provider: d.Provider, BaseURL: d.BaseURL, Model: d.Model, VisionModel: d.VisionModel,
		APIKeySet: d.APIKey != "", Source: sourceEnv,
	}, nil
}

// SaveSettings stores the server-wide model config. The request must
// already be validated.
func (a *Assistant) SaveSettings(ctx context.Context, userID string, req *models.AISettingsRequest) error {
	current, exists, err := a.loadSettings(ctx)
	if err != nil {
		return err
	}

	keyEnc := current.apiKeyEnc
	switch {
	case req.APIKey == nil && !exists:
		keyEnc = nil
	case req.APIKey == nil:
	case *req.APIKey == "":
		keyEnc = nil
	default:
		if keyEnc, err = a.seal(*req.APIKey); err != nil {
			return err
		}
	}
	if req.Provider == models.AIProviderAnthropic && keyEnc == nil {
		return ErrAPIKeyRequired
	}

	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO ai_settings
			(id, provider, base_url, model, vision_model, api_key_enc, updated_by, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			provider = excluded.provider, base_url = excluded.base_url,
			model = excluded.model, vision_model = excluded.vision_model,
			api_key_enc = excluded.api_key_enc,
			updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		req.Provider, req.BaseURL, req.Model, req.VisionModel, keyEnc, userID, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("save ai settings: %w", err)
	}
	return nil
}

// ResetSettings removes the saved config so the server default applies.
func (a *Assistant) ResetSettings(ctx context.Context) error {
	if _, err := a.db.ExecContext(ctx, `DELETE FROM ai_settings`); err != nil {
		return fmt.Errorf("reset ai settings: %w", err)
	}
	return nil
}

type settingsRow struct {
	provider, baseURL, model, visionModel string
	apiKeyEnc                             []byte
}

func (a *Assistant) loadSettings(ctx context.Context) (settingsRow, bool, error) {
	var r settingsRow
	err := a.db.QueryRowContext(ctx,
		`SELECT provider, base_url, model, vision_model, api_key_enc FROM ai_settings WHERE id = 1`,
	).Scan(&r.provider, &r.baseURL, &r.model, &r.visionModel, &r.apiKeyEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return settingsRow{}, false, nil
	}
	if err != nil {
		return settingsRow{}, false, fmt.Errorf("load ai settings: %w", err)
	}
	return r, true, nil
}

func (a *Assistant) savedConfig(ctx context.Context) (Config, bool, error) {
	r, ok, err := a.loadSettings(ctx)
	if err != nil || !ok {
		return Config{}, ok, err
	}
	cfg := Config{Provider: r.provider, BaseURL: r.baseURL, Model: r.model, VisionModel: r.visionModel}
	if r.apiKeyEnc != nil {
		if cfg.APIKey, err = a.open(r.apiKeyEnc); err != nil {
			return Config{}, false, err
		}
	}
	return cfg, true, nil
}

func newAEAD(secret []byte) (cipher.AEAD, error) {
	if len(secret) == 0 {
		return nil, errors.New("assistant: secret is required")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "medvault ai api key", 32)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return aead, nil
}

func (a *Assistant) seal(apiKey string) ([]byte, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	return a.aead.Seal(nonce, nonce, []byte(apiKey), nil), nil
}

func (a *Assistant) open(enc []byte) (string, error) {
	n := a.aead.NonceSize()
	if len(enc) < n {
		return "", ErrKeyUnreadable
	}
	plain, err := a.aead.Open(nil, enc[:n], enc[n:], nil)
	if err != nil {
		return "", ErrKeyUnreadable
	}
	return string(plain), nil
}
