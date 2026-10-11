// Package assistant answers questions about a household's medicines and
// drafts new entries from package photos, using a configurable LLM.
//
// The model is set by a server admin, falling back to the server defaults.
// All tools are read-only and scoped to one household.
package assistant

import (
	"context"
	"crypto/cipher"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/anthropic"
	"github.com/firebase/genkit/go/plugins/compat_oai/openai"
	"github.com/openai/openai-go/option"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/models"
)

var (
	// ErrDisabled means no model is configured.
	ErrDisabled = errors.New("assistant is not configured")
	// ErrKeyUnreadable means the stored API key can't be decrypted with the
	// current secret.
	ErrKeyUnreadable = errors.New("stored API key cannot be decrypted")
	// ErrUpstream means the model provider failed or returned nothing usable.
	ErrUpstream = errors.New("model provider request failed")
	// ErrAPIKeyRequired means the provider needs a key and none is stored.
	ErrAPIKeyRequired = errors.New("api_key is required for this provider")
	// ErrAPIKeyReentry means the provider or base URL changed while a key is
	// stored, so the key must be entered again or cleared.
	ErrAPIKeyReentry = errors.New("api_key must be re-entered when the provider or base_url changes")
)

// Config selects a model endpoint.
type Config struct {
	Provider    string // models.AIProviderOpenAI or models.AIProviderAnthropic
	BaseURL     string // empty means the provider's public API
	APIKey      string
	Model       string // empty disables the assistant
	VisionModel string // empty means Model
}

func (c *Config) visionModel() string {
	if c.VisionModel != "" {
		return c.VisionModel
	}
	return c.Model
}

// Options configures an Assistant.
type Options struct {
	Default     Config // used until an admin saves settings
	Secret      []byte // key material for encrypting the stored API key
	DrugCountry string // drug DB used for normalisation and similar drugs
	Timeout     time.Duration
}

// Assistant runs scans and chats against the configured model. It is safe
// for concurrent use.
type Assistant struct {
	db    *sql.DB
	drugs *database.DrugRegistry
	opts  Options
	aead  cipher.AEAD

	mu     sync.Mutex
	engine *engine
}

type engine struct {
	endpoint endpoint
	g        *genkit.Genkit
	tools    []ai.ToolRef
}

// endpoint identifies one Genkit instance; model names are chosen per call.
type endpoint struct {
	provider, baseURL, apiKey string
}

// New returns an Assistant. drugs may be nil.
func New(db *sql.DB, drugs *database.DrugRegistry, options *Options) (*Assistant, error) {
	opts := *options
	if opts.Default.Provider == "" {
		opts.Default.Provider = models.AIProviderOpenAI
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	aead, err := newAEAD(opts.Secret)
	if err != nil {
		return nil, err
	}
	return &Assistant{db: db, drugs: drugs, opts: opts, aead: aead}, nil
}

// Timeout is the upper bound for one Scan or Chat call.
func (a *Assistant) Timeout() time.Duration {
	return a.opts.Timeout
}

// Status describes whether the assistant can be used.
type Status struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"` // web | env | none
	Local   bool   `json:"local"`  // endpoint is on this machine or the LAN
}

// Status reports whether a usable model is configured.
func (a *Assistant) Status(ctx context.Context) (Status, error) {
	cfg, source, err := a.resolve(ctx)
	if errors.Is(err, ErrKeyUnreadable) {
		return Status{Source: sourceWeb}, nil
	}
	if err != nil {
		return Status{}, err
	}
	if cfg.Model == "" {
		return Status{Source: sourceNone}, nil
	}
	return Status{Enabled: true, Source: source, Local: isLocal(cfg.BaseURL)}, nil
}

const (
	sourceWeb  = "web"
	sourceEnv  = "env"
	sourceNone = "none"
)

// resolve returns the admin-saved Config, falling back to the server
// default. A saved Config never inherits the default API key.
func (a *Assistant) resolve(ctx context.Context) (Config, string, error) {
	cfg, ok, err := a.savedConfig(ctx)
	if err != nil {
		return Config{}, "", err
	}
	if ok {
		return cfg, sourceWeb, nil
	}
	if a.opts.Default.Model == "" {
		return Config{}, sourceNone, nil
	}
	return a.opts.Default, sourceEnv, nil
}

// prepare resolves the Config and its engine.
func (a *Assistant) prepare(ctx context.Context) (Config, *engine, error) {
	cfg, _, err := a.resolve(ctx)
	if err != nil {
		return Config{}, nil, err
	}
	if cfg.Model == "" {
		return Config{}, nil, ErrDisabled
	}
	e, err := a.engineFor(ctx, &cfg)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, e, nil
}

// engineFor returns the cached engine, rebuilding it when the endpoint
// changed.
func (a *Assistant) engineFor(ctx context.Context, cfg *Config) (*engine, error) {
	key := endpoint{cfg.Provider, cfg.BaseURL, cfg.APIKey}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engine != nil && a.engine.endpoint == key {
		return a.engine, nil
	}
	e, err := a.newEngine(ctx, cfg)
	if err != nil {
		return nil, err
	}
	e.endpoint = key
	a.engine = e
	return e, nil
}

func (a *Assistant) newEngine(ctx context.Context, cfg *Config) (e *engine, err error) {
	// Plugins panic on missing credentials instead of returning an error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("init %s plugin: %v", cfg.Provider, r)
		}
	}()

	var plugin api.Plugin
	switch cfg.Provider {
	case models.AIProviderAnthropic:
		plugin = &anthropic.Anthropic{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL}
	case models.AIProviderOpenAI:
		// Local servers ignore the key; a placeholder also stops the plugin
		// from falling back to OPENAI_API_KEY.
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = "none"
		}
		var reqOpts []option.RequestOption
		if cfg.BaseURL != "" {
			reqOpts = append(reqOpts, option.WithBaseURL(cfg.BaseURL+"/"))
		}
		plugin = &openai.OpenAI{APIKey: apiKey, Opts: reqOpts}
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}

	g := genkit.Init(context.WithoutCancel(ctx), genkit.WithPlugins(plugin))
	return &engine{g: g, tools: a.defineTools(g)}, nil
}

func modelName(cfg *Config, model string) string {
	return cfg.Provider + "/" + model
}

// isLocal reports whether baseURL points at a loopback, private or
// single-label host. An empty baseURL is the provider's public API.
func isLocal(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return host == "localhost" || !strings.Contains(host, ".") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".ts.net")
}
