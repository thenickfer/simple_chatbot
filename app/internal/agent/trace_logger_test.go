package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"

	"gpu-assistant/app/internal/store"
)

type traceFakeLLM struct {
	responses []*llms.ContentResponse
	callIdx   int
}

func (f *traceFakeLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	resp := f.responses[f.callIdx]
	f.callIdx++
	return resp, nil
}

func TestAgentTraceLogger_LogsMessagesThoughtsAndTools(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	fake := &traceFakeLLM{
		responses: []*llms.ContentResponse{
			{
				Choices: []*llms.ContentChoice{
					{
						StopReason:       "tool_calls",
						ReasoningContent: "Need to check the A100 specs in SQLite.",
						Content:          "Let me inspect the A100 inventory.",
						ToolCalls: []llms.ToolCall{
							{
								ID:   "call_1",
								Type: "function",
								FunctionCall: &llms.FunctionCall{
									Name:      "get_gpu",
									Arguments: `{"name":"A100"}`,
								},
							},
						},
					},
				},
			},
			{
				Choices: []*llms.ContentChoice{
					{
						StopReason: "stop",
						Content:    "<think>A100 has 80 GB VRAM.</think>\nThe NVIDIA A100 has 80 GB VRAM.",
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	tl := NewTraceLogger(&buf, "memory")

	ag := New(st, fake)
	ag.SetTraceLogger(tl)

	res, err := ag.Chat(ctx, "Tell me about the A100")
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if !strings.Contains(res.Reply, "NVIDIA A100") {
		t.Fatalf("unexpected reply: %q", res.Reply)
	}

	out := buf.String()
	expectedSubstrings := []string{
		"[TURN 1] [USER]",
		"Tell me about the A100",
		"[TURN 1] [LLM_REQUEST] phase=chat iter=1/4",
		"[TURN 1] [LLM_RESPONSE] iter=1/4",
		"[TURN 1] [THOUGHT] (source=reasoning_content)",
		"Need to check the A100 specs in SQLite.",
		"[TURN 1] [THOUGHT] (source=assistant_pre_tool)",
		"Let me inspect the A100 inventory.",
		"[TURN 1] [TOOL_CALL] id=call_1 name=get_gpu",
		`args: {"name":"A100"}`,
		"[TURN 1] [TOOL_RESULT] id=call_1 name=get_gpu status=ok",
		"[TURN 1] [THOUGHT] (source=think_tag)",
		"A100 has 80 GB VRAM.",
		"[TURN 1] [ASSISTANT]",
		"tools_called=[get_gpu]",
		"The NVIDIA A100 has 80 GB VRAM.",
	}
	for _, sub := range expectedSubstrings {
		if !strings.Contains(out, sub) {
			t.Errorf("expected trace log to contain %q, got:\n%s", sub, out)
		}
	}
}

func TestNewTraceLoggerFromEnv_CreatesTimestampedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_LOG_DIR", dir)
	t.Setenv("AGENT_LOG_PATH", "")
	t.Setenv("AGENT_LOG_DISABLED", "")

	tl, err := NewTraceLoggerFromEnv("gpu-assistant", "http://127.0.0.1:4000/v1")
	if err != nil {
		t.Fatalf("NewTraceLoggerFromEnv failed: %v", err)
	}
	if tl == nil {
		t.Fatal("expected non-nil TraceLogger")
	}
	tl.LogEvent("demo_check", "ok")
	if err := tl.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	base := filepath.Base(tl.Path())
	if !strings.HasPrefix(base, "agent-") || !strings.HasSuffix(base, ".log") {
		t.Fatalf("expected timestamped agent-*.log filename, got %q", base)
	}

	data, err := os.ReadFile(tl.Path())
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[SESSION] started | model=gpu-assistant base_url=http://127.0.0.1:4000/v1") {
		t.Errorf("missing session header in log file:\n%s", content)
	}
	if !strings.Contains(content, "[EVENT] demo_check | ok") {
		t.Errorf("missing event in log file:\n%s", content)
	}
}

func TestNewTraceLoggerFromEnv_Disabled(t *testing.T) {
	t.Setenv("AGENT_LOG_DISABLED", "true")
	tl, err := NewTraceLoggerFromEnv("gpu-assistant", "http://127.0.0.1:4000/v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tl != nil {
		t.Fatalf("expected nil TraceLogger when disabled, got %v", tl)
	}
}
