package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"gpu-assistant/app/internal/agent"
	"gpu-assistant/app/internal/llm"
	"gpu-assistant/app/internal/store"
)

// MessageDTO represents a single conversation turn returned to the frontend.
type MessageDTO struct {
	ID        int64    `json:"id"`
	Role      string   `json:"role"`
	Content   string   `json:"content"`
	ToolCalls []string `json:"tool_calls,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// ChatResponse is returned by SendMessage and POST /api/chat.
type ChatResponse struct {
	Reply     string     `json:"reply"`
	Message   MessageDTO `json:"message"`
	ToolCalls []string   `json:"tool_calls,omitempty"`
	Model     string     `json:"model"`
}

// StatusDTO represents the backend, SQLite, and LiteLLM proxy status.
type StatusDTO struct {
	Online    bool   `json:"online"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	GPUCount  int    `json:"gpu_count"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// App is the Wails-bound application struct and HTTP JSON API handler.
type App struct {
	ctx       context.Context
	mu        sync.Mutex
	store     *store.Store
	llmClient *llm.Client
	agent     *agent.Agent
}

// NewApp creates and initializes a new App instance with SQLite store, LiteLLM client, and Agent.
func NewApp() (*App, error) {
	llm.LoadDotEnvFiles()
	ctx := context.Background()
	st, err := store.OpenFromEnv(ctx)
	if err != nil {
		return nil, fmt.Errorf("init sqlite store: %w", err)
	}

	llmClient, err := llm.NewClientFromEnv()
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("init litellm client: %w", err)
	}

	ag := agent.New(st, llmClient)
	return &App{
		ctx:       ctx,
		store:     st,
		llmClient: llmClient,
		agent:     ag,
	}, nil
}

// NewAppWithDeps constructs an App with explicit store, llmClient, and agent (for testing).
func NewAppWithDeps(st *store.Store, llmClient *llm.Client, ag *agent.Agent) *App {
	return &App{
		ctx:       context.Background(),
		store:     st,
		llmClient: llmClient,
		agent:     ag,
	}
}

// startup is called when the Wails application starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// shutdown is called when the Wails application exits.
func (a *App) shutdown(_ context.Context) {
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *App) baseContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// SendMessage sends a user prompt through the LangChainGo GPU agent and persists the turn in SQLite.
func (a *App) SendMessage(text string) (ChatResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	timeout := llm.DefaultRequestTimeout
	if a.llmClient != nil {
		timeout = a.llmClient.Timeout()
	}
	ctx, cancel := context.WithTimeout(a.baseContext(), timeout)
	defer cancel()

	res, err := a.agent.Chat(ctx, text)
	if err != nil {
		return ChatResponse{}, err
	}

	modelName := llm.DefaultModel
	if a.llmClient != nil {
		modelName = a.llmClient.Model()
	}

	dto := toMessageDTO(res.Message)
	return ChatResponse{
		Reply:     res.Reply,
		Message:   dto,
		ToolCalls: res.ToolCalls,
		Model:     modelName,
	}, nil
}

// GetHistory returns all persisted conversation messages from SQLite in chronological order.
func (a *App) GetHistory() ([]MessageDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	stored, err := a.store.ListStoredMessages(a.baseContext())
	if err != nil {
		return nil, err
	}

	out := make([]MessageDTO, 0, len(stored))
	for _, m := range stored {
		out = append(out, toMessageDTO(m))
	}
	return out, nil
}

// ClearConversation deletes all messages from the SQLite conversation history.
func (a *App) ClearConversation() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.store.Clear(a.baseContext())
}

// GetStatus checks LiteLLM reachability, active model alias, and seeded GPU count.
func (a *App) GetStatus() (StatusDTO, error) {
	ctx := a.baseContext()

	var gpuCount int
	if a.store != nil {
		if gpus, err := a.store.ListGPUs(ctx); err == nil {
			gpuCount = len(gpus)
		}
	}

	if a.llmClient == nil {
		return StatusDTO{
			Online:   false,
			Model:    llm.DefaultModel,
			BaseURL:  llm.DefaultBaseURL,
			GPUCount: gpuCount,
			Error:    "LiteLLM client not initialized",
		}, nil
	}

	h := a.llmClient.CheckHealth(ctx)
	return StatusDTO{
		Online:    h.Online,
		Model:     h.Model,
		BaseURL:   h.BaseURL,
		GPUCount:  gpuCount,
		LatencyMs: h.LatencyMs,
		Error:     h.Error,
	}, nil
}

// APIHandler returns an http.Handler exposing /api/status, /api/history, /api/chat, and /api/clear,
// and optionally delegating non-/api requests to fallback (e.g., Wails asset handler or static files).
func (a *App) APIHandler(fallback http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		status, err := a.GetStatus()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.Method {
		case http.MethodGet:
			history, err := a.GetHistory()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, history)
		case http.MethodDelete:
			if err := a.ClearConversation(); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Text    string `json:"text"`
			Message string `json:"message"`
			Prompt  string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		prompt := strings.TrimSpace(req.Text)
		if prompt == "" {
			prompt = strings.TrimSpace(req.Message)
		}
		if prompt == "" {
			prompt = strings.TrimSpace(req.Prompt)
		}
		if prompt == "" {
			writeJSONError(w, http.StatusBadRequest, "message text is required")
			return
		}

		resp, err := a.SendMessage(prompt)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/api/clear", func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := a.ClearConversation(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			mux.ServeHTTP(w, r)
			return
		}
		if fallback != nil {
			fallback.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

func toMessageDTO(m store.StoredMessage) MessageDTO {
	ts := m.CreatedAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return MessageDTO{
		ID:        m.ID,
		Role:      m.Role,
		Content:   m.Content,
		ToolCalls: m.ToolCalls,
		CreatedAt: ts.UTC().Format(time.RFC3339),
	}
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
