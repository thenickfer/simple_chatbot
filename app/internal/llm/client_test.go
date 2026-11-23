package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gpu-assistant/app/internal/llm"
)

func TestClientCheckHealth_SuccessLiveliness(t *testing.T) {
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

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := llm.NewClient(llm.Config{
		BaseURL:        srv.URL + "/v1",
		APIKey:         "test-key",
		Model:          "gpu-assistant",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	status := client.CheckHealth(context.Background())
	if !status.Online {
		t.Fatalf("expected status.Online == true, got false (error: %s)", status.Error)
	}
	if status.Error != "" {
		t.Errorf("expected empty status.Error, got %q", status.Error)
	}
	if status.Model != "gpu-assistant" {
		t.Errorf("expected model 'gpu-assistant', got %q", status.Model)
	}
}

func TestClientCheckHealth_FallbackToModelsWhenLivelinessFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/liveliness", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpu-assistant","object":"model"}]}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := llm.NewClient(llm.Config{
		BaseURL:        srv.URL + "/v1",
		APIKey:         "test-key",
		Model:          "gpu-assistant",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	status := client.CheckHealth(context.Background())
	if !status.Online {
		t.Fatalf("expected status.Online == true on fallback to /v1/models, got false (error: %s)", status.Error)
	}
}

func TestClientCheckHealth_Unreachable(t *testing.T) {
	client, err := llm.NewClient(llm.Config{
		BaseURL:        "http://127.0.0.1:59999/v1", // Unreachable port
		APIKey:         "test-key",
		Model:          "gpu-assistant",
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	status := client.CheckHealth(ctx)
	if status.Online {
		t.Fatalf("expected status.Online == false for unreachable server, got true")
	}
	if status.Error == "" {
		t.Errorf("expected non-empty status.Error for unreachable server")
	}
}
