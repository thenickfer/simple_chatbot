package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// openAIChatRequest models the wire payload sent by langchaingo/llms/openai to /v1/chat/completions.
type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
	Tools    []map[string]any    `json:"tools,omitempty"`
}

type openAIChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// startMockLiteLLMServer starts an OpenAI-compatible HTTP server simulating the LiteLLM proxy
// (/health/liveliness, /v1/models, /v1/chat/completions) for end-to-end wire verification.
func startMockLiteLLMServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/health/liveliness", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"I'm alive!"`))
	})

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpu-assistant","object":"model"}]}`))
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Messages) == 0 {
			http.Error(w, "empty messages", http.StatusBadRequest)
			return
		}

		last := req.Messages[len(req.Messages)-1]

		// Step 2 of tool loop: receive role="tool" output and synthesize factual reply strictly from SQLite data.
		if last.Role == "tool" {
			var userQuestion string
			var toolName string
			for i := len(req.Messages) - 1; i >= 0; i-- {
				if req.Messages[i].Role == "assistant" && len(req.Messages[i].ToolCalls) > 0 && toolName == "" {
					toolName = req.Messages[i].ToolCalls[0].Function.Name
				}
				if req.Messages[i].Role == "user" {
					userQuestion = req.Messages[i].Content
					break
				}
			}

			reply := synthesizeWireReply(userQuestion, toolName, last.Content)
			resp := map[string]any{
				"id":      "chatcmpl-e2e-2",
				"object":  "chat.completion",
				"model":   req.Model,
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": reply,
						},
						"finish_reason": "stop",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Step 1 of tool loop: inspect user prompt and emit an OpenAI tool_calls response.
		lower := strings.ToLower(last.Content)
		toolName := "list_gpus"
		toolArgs := `{}`

		switch {
		case strings.Contains(lower, "duas a100") || (strings.Contains(lower, "a100") && strings.Contains(lower, "dispon")):
			toolName = "check_availability"
			toolArgs = `{"name":"A100","quantity":2}`
		case strings.Contains(lower, "b200"):
			toolName = "get_gpu"
			toolArgs = `{"name":"B200"}`
		case strings.Contains(lower, "most memory"):
			toolName = "list_gpus"
			toolArgs = `{}`
		default:
			toolName = "list_gpus"
			toolArgs = `{}`
		}

		resp := map[string]any{
			"id":     "chatcmpl-e2e-1",
			"object": "chat.completion",
			"model":  req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_e2e_1",
								"type": "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func synthesizeWireReply(question, toolName, toolJSON string) string {
	lowerQ := strings.ToLower(question)

	switch toolName {
	case "list_gpus":
		var payload struct {
			GPUs []struct {
				Name          string `json:"name"`
				Architecture  string `json:"architecture"`
				MemoryGB      int    `json:"memory_gb"`
				TotalQuantity int    `json:"total_quantity"`
				InUse         int    `json:"in_use"`
				Available     int    `json:"available"`
			} `json:"gpus"`
			Summary struct {
				MaxPerGPUMemoryGB            int    `json:"max_per_gpu_memory_gb"`
				HighestAvailableAggregateGPU string `json:"highest_available_aggregate_vram_gpu"`
			} `json:"summary"`
		}
		_ = json.Unmarshal([]byte(toolJSON), &payload)

		if strings.Contains(lowerQ, "most memory") {
			return fmt.Sprintf(
				"Per GPU, **NVIDIA A100** and **NVIDIA H100** have the highest memory at **%d GB VRAM** each (both currently have 2 units available = 160 GB available VRAM each; A100 has 4 total = 320 GB installed). By aggregate currently available VRAM pool, **%s** leads.",
				payload.Summary.MaxPerGPUMemoryGB,
				payload.Summary.HighestAvailableAggregateGPU,
			)
		}

		var lines []string
		lines = append(lines, fmt.Sprintf("Existem %d modelos de GPU cadastrados na infraestrutura:", len(payload.GPUs)))
		for _, g := range payload.GPUs {
			lines = append(lines, fmt.Sprintf("- **%s** (%s, %d GB VRAM): %d total (%d em uso, %d disponível)",
				g.Name, g.Architecture, g.MemoryGB, g.TotalQuantity, g.InUse, g.Available))
		}
		return strings.Join(lines, "\n")

	case "check_availability":
		var payload struct {
			GPUName        string `json:"gpu_name"`
			MemoryGB       int    `json:"memory_gb"`
			Available      bool   `json:"available"`
			AvailableCount int    `json:"available_count"`
			InUse          int    `json:"in_use"`
			TotalQuantity  int    `json:"total_quantity"`
		}
		_ = json.Unmarshal([]byte(toolJSON), &payload)
		if payload.Available {
			return fmt.Sprintf(
				"Sim! Existem **%d** unidades da **%s** (%d GB VRAM cada) disponíveis no momento, de um total de %d instaladas (%d em uso).",
				payload.AvailableCount, payload.GPUName, payload.MemoryGB, payload.TotalQuantity, payload.InUse,
			)
		}
		return fmt.Sprintf("Não, apenas %d unidades da %s estão disponíveis.", payload.AvailableCount, payload.GPUName)

	case "get_gpu":
		var payload struct {
			Found          bool     `json:"found"`
			Query          string   `json:"query"`
			Message        string   `json:"message"`
			ExistingModels []string `json:"existing_models"`
		}
		_ = json.Unmarshal([]byte(toolJSON), &payload)
		if !payload.Found {
			return fmt.Sprintf(
				"No, **%s** is not in our infrastructure (%s). The models currently in the catalog are: %s.",
				payload.Query, payload.Message, strings.Join(payload.ExistingModels, ", "),
			)
		}
		return payload.Message
	}

	return toolJSON
}

func TestEndToEndAppFourConversationsAndPersistence(t *testing.T) {
	mockLiteLLM := startMockLiteLLMServer(t)
	dbPath := filepath.Join(t.TempDir(), "e2e_gpu_assistant.db")

	t.Setenv("LITELLM_BASE_URL", mockLiteLLM.URL+"/v1")
	t.Setenv("LITELLM_API_KEY", "sk-test-e2e")
	t.Setenv("LITELLM_MODEL", "gpu-assistant")
	t.Setenv("SQLITE_PATH", dbPath)

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	ctx := context.Background()
	app.startup(ctx)

	httpSrv := httptest.NewServer(app.APIHandler(http.FileServer(http.FS(resolveFrontendFS()))))
	defer httpSrv.Close()

	// 1. Verify /api/status and GetStatus()
	statusResp, err := http.Get(httpSrv.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status failed: %v", err)
	}
	defer statusResp.Body.Close()
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status status = %d, want 200", statusResp.StatusCode)
	}
	var status StatusDTO
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatalf("decode /api/status: %v", err)
	}
	if !status.Online || status.Model != "gpu-assistant" || status.GPUCount != 5 {
		t.Fatalf("unexpected /api/status: %+v", status)
	}

	// 2. Verify embedded frontend index.html is served at /
	indexResp, err := http.Get(httpSrv.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer indexResp.Body.Close()
	indexBytes, _ := io.ReadAll(indexResp.Body)
	if indexResp.StatusCode != http.StatusOK || !strings.Contains(string(indexBytes), "<div id=\"root\"></div>") {
		t.Fatalf("expected embedded frontend index.html at /, got status=%d body=%q", indexResp.StatusCode, string(indexBytes))
	}

	// Helper to post to /api/chat
	postChat := func(prompt string) ChatResponse {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"text": prompt})
		resp, err := http.Post(httpSrv.URL+"/api/chat", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/chat(%q) failed: %v", prompt, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("POST /api/chat(%q) status = %d: %s", prompt, resp.StatusCode, string(raw))
		}
		var cr ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			t.Fatalf("decode ChatResponse: %v", err)
		}
		return cr
	}

	// Conversation 1: "Quais GPUs existem na infraestrutura?"
	c1 := postChat("Quais GPUs existem na infraestrutura?")
	if len(c1.ToolCalls) != 1 || c1.ToolCalls[0] != "list_gpus" {
		t.Errorf("C1 tool_calls = %v, want [list_gpus]", c1.ToolCalls)
	}
	for _, want := range []string{"NVIDIA A100", "NVIDIA L40S", "NVIDIA RTX 4090", "NVIDIA H100", "NVIDIA A40", "80 GB", "48 GB", "24 GB"} {
		if !strings.Contains(c1.Reply, want) {
			t.Errorf("C1 reply missing %q: %s", want, c1.Reply)
		}
	}

	// Conversation 2: "Existem duas A100 disponíveis?"
	c2 := postChat("Existem duas A100 disponíveis?")
	if len(c2.ToolCalls) != 1 || c2.ToolCalls[0] != "check_availability" {
		t.Errorf("C2 tool_calls = %v, want [check_availability]", c2.ToolCalls)
	}
	for _, want := range []string{"Sim", "2", "NVIDIA A100", "80 GB", "4"} {
		if !strings.Contains(c2.Reply, want) {
			t.Errorf("C2 reply missing %q: %s", want, c2.Reply)
		}
	}

	// Conversation 3: "Which GPU has the most memory available?"
	c3 := postChat("Which GPU has the most memory available?")
	if len(c3.ToolCalls) != 1 || c3.ToolCalls[0] != "list_gpus" {
		t.Errorf("C3 tool_calls = %v, want [list_gpus]", c3.ToolCalls)
	}
	for _, want := range []string{"NVIDIA A100", "NVIDIA H100", "80 GB", "160 GB", "NVIDIA L40S", "240 GB"} {
		if !strings.Contains(c3.Reply, want) {
			t.Errorf("C3 reply missing %q: %s", want, c3.Reply)
		}
	}

	// Conversation 4: "Do you have a B200?" (via Wails binding SendMessage directly to verify binding + HTTP parity)
	c4, err := app.SendMessage("Do you have a B200?")
	if err != nil {
		t.Fatalf("SendMessage(B200) failed: %v", err)
	}
	if len(c4.ToolCalls) != 1 || c4.ToolCalls[0] != "get_gpu" {
		t.Errorf("C4 tool_calls = %v, want [get_gpu]", c4.ToolCalls)
	}
	for _, want := range []string{"No", "B200", "not in our infrastructure", "GPU not found in infrastructure", "NVIDIA A100"} {
		if !strings.Contains(c4.Reply, want) {
			t.Errorf("C4 reply missing %q: %s", want, c4.Reply)
		}
	}

	// 3. Close the first App instance and reopen a new App instance on the same SQLite database file
	app.shutdown(ctx)

	app2, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp reopen failed: %v", err)
	}
	app2.startup(ctx)
	defer app2.shutdown(ctx)

	history, err := app2.GetHistory()
	if err != nil {
		t.Fatalf("app2.GetHistory failed: %v", err)
	}
	if len(history) != 8 {
		t.Fatalf("expected 8 persisted messages across SQLite reopen, got %d", len(history))
	}
	if history[0].Role != "user" || history[0].Content != "Quais GPUs existem na infraestrutura?" {
		t.Errorf("unexpected history[0]: %+v", history[0])
	}
	if history[7].Role != "assistant" || !strings.Contains(history[7].Content, "B200") {
		t.Errorf("unexpected history[7]: %+v", history[7])
	}

	// 4. Clear conversation via /api/clear and verify history is empty while 5 GPUs remain intact
	httpSrv2 := httptest.NewServer(app2.APIHandler(nil))
	defer httpSrv2.Close()

	clearResp, err := http.Post(httpSrv2.URL+"/api/clear", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/clear failed: %v", err)
	}
	defer clearResp.Body.Close()
	if clearResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/clear status = %d, want 200", clearResp.StatusCode)
	}

	afterClear, err := app2.GetHistory()
	if err != nil {
		t.Fatalf("GetHistory after clear failed: %v", err)
	}
	if len(afterClear) != 0 {
		t.Fatalf("expected 0 messages after ClearConversation, got %d", len(afterClear))
	}

	statusAfterClear, err := app2.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus after clear failed: %v", err)
	}
	if statusAfterClear.GPUCount != 5 {
		t.Fatalf("expected 5 seeded GPUs to remain intact after ClearConversation, got %d", statusAfterClear.GPUCount)
	}
}
