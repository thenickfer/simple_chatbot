package llm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

const (
	DefaultBaseURL     = "http://127.0.0.1:4000/v1"
	DefaultAPIKey      = "sk-local-gpu-assistant"
	DefaultModel       = "gpu-assistant"
	HealthCheckTimeout = 2500 * time.Millisecond
)

// Config holds the LiteLLM proxy connection parameters.
type Config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"-"`
	Model   string `json:"model"`
}

// LoadConfigFromEnv loads LiteLLM configuration from environment variables,
// falling back to sensible local defaults matching .env.example.
func LoadConfigFromEnv() Config {
	baseURL := strings.TrimSpace(os.Getenv("LITELLM_BASE_URL"))
	if baseURL == "" {
		port := strings.TrimSpace(os.Getenv("LITELLM_PORT"))
		if port != "" {
			baseURL = fmt.Sprintf("http://127.0.0.1:%s/v1", port)
		} else {
			baseURL = DefaultBaseURL
		}
	}

	apiKey := strings.TrimSpace(os.Getenv("LITELLM_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("LITELLM_MASTER_KEY"))
	}
	if apiKey == "" {
		apiKey = DefaultAPIKey
	}

	model := strings.TrimSpace(os.Getenv("LITELLM_MODEL"))
	if model == "" {
		model = DefaultModel
	}

	return Config{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
	}
}

// HealthStatus reports whether the LiteLLM proxy is reachable.
type HealthStatus struct {
	Online    bool   `json:"online"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Client wraps a langchaingo OpenAI-compatible LLM pointed at the LiteLLM proxy.
type Client struct {
	cfg        Config
	llm        *openai.LLM
	httpClient *http.Client
}

// NewClient initializes a LiteLLM client using langchaingo/llms/openai.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg.APIKey = DefaultAPIKey
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	model, err := openai.New(
		openai.WithBaseURL(cfg.BaseURL),
		openai.WithToken(cfg.APIKey),
		openai.WithModel(cfg.Model),
	)
	if err != nil {
		return nil, fmt.Errorf("create langchaingo openai client: %w", err)
	}

	return &Client{
		cfg: cfg,
		llm: model,
		httpClient: &http.Client{
			Timeout: HealthCheckTimeout,
		},
	}, nil
}

// NewClientFromEnv creates a LiteLLM client configured from environment variables.
func NewClientFromEnv() (*Client, error) {
	return NewClient(LoadConfigFromEnv())
}

// Config returns the active LiteLLM configuration.
func (c *Client) Config() Config {
	return c.cfg
}

// Model returns the configured model alias.
func (c *Client) Model() string {
	return c.cfg.Model
}

// GenerateContent delegates to langchaingo's openai.LLM.GenerateContent.
func (c *Client) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return c.llm.GenerateContent(ctx, messages, options...)
}

// CheckHealth probes the LiteLLM proxy (/health/liveliness and /v1/models) with a fast timeout (2.5s)
// so the Wails UI never blocks or crashes when the proxy is offline.
func (c *Client) CheckHealth(ctx context.Context) HealthStatus {
	start := time.Now()
	status := HealthStatus{
		Online:  false,
		BaseURL: c.cfg.BaseURL,
		Model:   c.cfg.Model,
	}

	checkCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeout)
	defer cancel()

	rootURL := strings.TrimSuffix(c.cfg.BaseURL, "/v1")
	endpoints := []string{
		rootURL + "/health/liveliness",
		c.cfg.BaseURL + "/models",
	}

	var lastErr error
	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, ep, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if c.cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			status.Online = true
			status.LatencyMs = time.Since(start).Milliseconds()
			status.Error = ""
			return status
		}
		lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, ep)
	}

	status.LatencyMs = time.Since(start).Milliseconds()
	if lastErr != nil {
		status.Error = lastErr.Error()
	} else {
		status.Error = "LiteLLM proxy unreachable"
	}
	return status
}
