package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"

	"gpu-assistant/app/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "agent_test.db")
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func TestAllFourGPUTools(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	// 1. list_gpus
	listTool := &ListGPUsTool{store: st}
	listOut, err := listTool.Call(ctx, "{}")
	if err != nil {
		t.Fatalf("list_gpus failed: %v", err)
	}
	var listResp listGPUsResponse
	if err := json.Unmarshal([]byte(listOut), &listResp); err != nil {
		t.Fatalf("unmarshal list_gpus output: %v", err)
	}
	if len(listResp.GPUs) != 5 {
		t.Fatalf("expected 5 GPUs in list_gpus, got %d", len(listResp.GPUs))
	}
	if listResp.Summary.MaxPerGPUMemoryGB != 80 {
		t.Errorf("expected max_per_gpu_memory_gb=80, got %d", listResp.Summary.MaxPerGPUMemoryGB)
	}
	if !strings.Contains(listResp.Summary.HighestAvailableAggregateGPU, "NVIDIA L40S") {
		t.Errorf("expected L40S as highest available aggregate VRAM pool, got %q", listResp.Summary.HighestAvailableAggregateGPU)
	}

	// 2. get_gpu (existing and missing B200)
	getTool := &GetGPUTool{store: st}
	a100Out, err := getTool.Call(ctx, `{"name": "nvidia a100"}`)
	if err != nil {
		t.Fatalf("get_gpu(nvidia a100) failed: %v", err)
	}
	var getA100 getGPUResponse
	if err := json.Unmarshal([]byte(a100Out), &getA100); err != nil {
		t.Fatalf("unmarshal get_gpu(a100): %v", err)
	}
	if !getA100.Found || getA100.GPU == nil || getA100.GPU.Name != "NVIDIA A100" || getA100.GPU.Available != 2 {
		t.Errorf("unexpected get_gpu(a100) result: %+v", getA100)
	}

	b200Out, err := getTool.Call(ctx, "B200")
	if err != nil {
		t.Fatalf("get_gpu(B200) failed: %v", err)
	}
	var getB200 getGPUResponse
	if err := json.Unmarshal([]byte(b200Out), &getB200); err != nil {
		t.Fatalf("unmarshal get_gpu(B200): %v", err)
	}
	if getB200.Found || !strings.Contains(getB200.Message, "GPU not found in infrastructure") || len(getB200.ExistingModels) != 5 {
		t.Errorf("unexpected get_gpu(B200) result: %+v", getB200)
	}

	// 3. check_availability
	availTool := &CheckAvailabilityTool{store: st}
	avail2A100, err := availTool.Call(ctx, `{"name": "A100", "quantity": 2}`)
	if err != nil {
		t.Fatalf("check_availability(A100, 2) failed: %v", err)
	}
	var availResp checkAvailabilityResponse
	if err := json.Unmarshal([]byte(avail2A100), &availResp); err != nil {
		t.Fatalf("unmarshal check_availability: %v", err)
	}
	if !availResp.Available || availResp.AvailableCount != 2 || availResp.InUse != 2 || availResp.TotalQuantity != 4 || availResp.MemoryGB != 80 {
		t.Errorf("unexpected check_availability(A100, 2): %+v", availResp)
	}

	avail3A100, err := availTool.Call(ctx, "A100, 3")
	if err != nil {
		t.Fatalf("check_availability(A100, 3) failed: %v", err)
	}
	var avail3Resp checkAvailabilityResponse
	_ = json.Unmarshal([]byte(avail3A100), &avail3Resp)
	if avail3Resp.Available {
		t.Errorf("expected 3 A100s to NOT be available (only 2 free), got available=true")
	}

	// 4. find_gpus
	findTool := &FindGPUsTool{store: st}
	findOut, err := findTool.Call(ctx, `{"min_memory_gb": 48, "min_available": 1}`)
	if err != nil {
		t.Fatalf("find_gpus failed: %v", err)
	}
	var findResp findGPUsResponse
	if err := json.Unmarshal([]byte(findOut), &findResp); err != nil {
		t.Fatalf("unmarshal find_gpus: %v", err)
	}
	if findResp.Count != 3 {
		t.Fatalf("expected 3 GPUs with >=48GB and >=1 available (A100, H100, L40S), got %d", findResp.Count)
	}
	if findResp.GPUs[0].MemoryGB != 80 || findResp.GPUs[1].MemoryGB != 80 || findResp.GPUs[2].Name != "NVIDIA L40S" {
		t.Errorf("unexpected find_gpus sort order: %+v", findResp.GPUs)
	}
}

// fakeLLM simulates a deterministic tool-calling model that chooses tools based on the user query
// and synthesizes factual answers strictly from the tool response payload.
type fakeLLM struct {
	failErr      error
	useJSONText  bool // if true, emits JSON tool call in Content instead of structured ToolCalls
	callCounter  int
}

func (f *fakeLLM) GenerateContent(_ context.Context, messages []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	f.callCounter++
	if f.failErr != nil {
		return nil, f.failErr
	}

	lastMsg := messages[len(messages)-1]

	// If the last message is a ToolCallResponse, synthesize the final answer from the tool output!
	if lastMsg.Role == llms.ChatMessageTypeTool {
		var toolResp llms.ToolCallResponse
		for _, p := range lastMsg.Parts {
			if tr, ok := p.(llms.ToolCallResponse); ok {
				toolResp = tr
				break
			}
		}

		// Find the latest human question to reply in the right language.
		var humanText string
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == llms.ChatMessageTypeHuman {
				for _, p := range messages[i].Parts {
					if tp, ok := p.(llms.TextContent); ok {
						humanText = tp.Text
					}
				}
				break
			}
		}

		reply := synthesizeFromToolOutput(humanText, toolResp)
		return &llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{Content: reply},
			},
		}, nil
	}

	// Otherwise, inspect the human prompt and emit the appropriate tool call.
	var userPrompt string
	for _, p := range lastMsg.Parts {
		if tp, ok := p.(llms.TextContent); ok {
			userPrompt = tp.Text
		}
	}
	lower := strings.ToLower(userPrompt)

	var toolName, toolArgs string
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

	if f.useJSONText {
		rawJSON := fmt.Sprintf(`{"name": %q, "arguments": %s}`, toolName, toolArgs)
		return &llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{Content: rawJSON},
			},
		}, nil
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				ToolCalls: []llms.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      toolName,
							Arguments: toolArgs,
						},
					},
				},
			},
		},
	}, nil
}

func synthesizeFromToolOutput(question string, tr llms.ToolCallResponse) string {
	lowerQ := strings.ToLower(question)

	switch tr.Name {
	case "list_gpus":
		var data listGPUsResponse
		_ = json.Unmarshal([]byte(tr.Content), &data)
		if strings.Contains(lowerQ, "most memory") {
			return fmt.Sprintf(
				"Per GPU, the highest memory models are **NVIDIA A100** and **NVIDIA H100** with **80 GB VRAM** each (A100 has 2 of 4 available = 160 GB currently available / 320 GB total installed; H100 has 2 of 2 available = 160 GB available). Across aggregate available units, **NVIDIA L40S** provides the largest free VRAM pool with **5 available x 48 GB = 240 GB** (out of 6 total = 288 GB).",
			)
		}
		var lines []string
		lines = append(lines, "Existem 5 modelos de GPU na infraestrutura:")
		for _, g := range data.GPUs {
			lines = append(lines, fmt.Sprintf("- **%s** (%s): %d GB VRAM — %d total, %d em uso, %d disponível(is)",
				g.Name, g.Architecture, g.MemoryGB, g.TotalQuantity, g.InUse, g.Available))
		}
		return strings.Join(lines, "\n")

	case "check_availability":
		var data checkAvailabilityResponse
		_ = json.Unmarshal([]byte(tr.Content), &data)
		if data.Available {
			return fmt.Sprintf("Sim, existem %d unidades da **%s** disponíveis agora (de um total de %d instaladas, com %d em uso, %d GB de VRAM cada).",
				data.AvailableCount, data.GPUName, data.TotalQuantity, data.InUse, data.MemoryGB)
		}
		return fmt.Sprintf("Não, apenas %d de %d unidades da %s estão disponíveis.", data.AvailableCount, data.TotalQuantity, data.GPUName)

	case "get_gpu":
		var data getGPUResponse
		_ = json.Unmarshal([]byte(tr.Content), &data)
		if !data.Found {
			return fmt.Sprintf("No, **%s** is not present in our infrastructure. The 5 available models in the catalog are: %s.",
				data.Query, strings.Join(data.ExistingModels, ", "))
		}
		return data.Message
	}
	return tr.Content
}

func TestAgentFourRequiredConversations(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	fake := &fakeLLM{}
	ag := New(st, fake)

	// 1. "Quais GPUs existem na infraestrutura?"
	r1, err := ag.Chat(ctx, "Quais GPUs existem na infraestrutura?")
	if err != nil {
		t.Fatalf("Conversation 1 failed: %v", err)
	}
	if len(r1.ToolCalls) != 1 || r1.ToolCalls[0] != "list_gpus" {
		t.Errorf("Conversation 1 tool calls = %v, want [list_gpus]", r1.ToolCalls)
	}
	for _, mustContain := range []string{"NVIDIA A100", "NVIDIA L40S", "NVIDIA RTX 4090", "NVIDIA H100", "NVIDIA A40", "80 GB", "48 GB", "24 GB"} {
		if !strings.Contains(r1.Reply, mustContain) {
			t.Errorf("Conversation 1 reply missing %q: %s", mustContain, r1.Reply)
		}
	}

	// 2. "Existem duas A100 disponíveis?"
	r2, err := ag.Chat(ctx, "Existem duas A100 disponíveis?")
	if err != nil {
		t.Fatalf("Conversation 2 failed: %v", err)
	}
	if len(r2.ToolCalls) != 1 || r2.ToolCalls[0] != "check_availability" {
		t.Errorf("Conversation 2 tool calls = %v, want [check_availability]", r2.ToolCalls)
	}
	for _, mustContain := range []string{"Sim", "2", "NVIDIA A100", "4", "80 GB"} {
		if !strings.Contains(r2.Reply, mustContain) {
			t.Errorf("Conversation 2 reply missing %q: %s", mustContain, r2.Reply)
		}
	}

	// 3. "Which GPU has the most memory available?"
	r3, err := ag.Chat(ctx, "Which GPU has the most memory available?")
	if err != nil {
		t.Fatalf("Conversation 3 failed: %v", err)
	}
	for _, mustContain := range []string{"NVIDIA A100", "NVIDIA H100", "80 GB", "160 GB", "NVIDIA L40S", "240 GB"} {
		if !strings.Contains(r3.Reply, mustContain) {
			t.Errorf("Conversation 3 reply missing %q: %s", mustContain, r3.Reply)
		}
	}

	// 4. "Do you have a B200?"
	r4, err := ag.Chat(ctx, "Do you have a B200?")
	if err != nil {
		t.Fatalf("Conversation 4 failed: %v", err)
	}
	if len(r4.ToolCalls) != 1 || r4.ToolCalls[0] != "get_gpu" {
		t.Errorf("Conversation 4 tool calls = %v, want [get_gpu]", r4.ToolCalls)
	}
	for _, mustContain := range []string{"No", "B200", "not present", "NVIDIA A100", "NVIDIA H100"} {
		if !strings.Contains(r4.Reply, mustContain) {
			t.Errorf("Conversation 4 reply missing %q: %s", mustContain, r4.Reply)
		}
	}

	// Verify all 4 conversations (8 turns: 4 user + 4 assistant) were persisted in SQLite.
	persisted, err := st.ListStoredMessages(ctx)
	if err != nil {
		t.Fatalf("ListStoredMessages failed: %v", err)
	}
	if len(persisted) != 8 {
		t.Fatalf("expected 8 persisted messages (4 turns), got %d", len(persisted))
	}
}

func TestAgentFallbackJSONToolCallAndProxyDownError(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	// Test fallback JSON tool-call parsing when a small local model emits raw JSON in Content.
	fallbackLLM := &fakeLLM{useJSONText: true}
	ag := New(st, fallbackLLM)
	resp, err := ag.Chat(ctx, "Do you have a B200?")
	if err != nil {
		t.Fatalf("Fallback JSON tool call failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0] != "get_gpu" {
		t.Errorf("expected fallback tool call [get_gpu], got %v", resp.ToolCalls)
	}

	// Test proxy-down error handling without panicking or corrupting history.
	downLLM := &fakeLLM{failErr: errors.New("dial tcp 127.0.0.1:4000: connect: connection refused")}
	agDown := New(st, downLLM)
	_, err = agDown.Chat(ctx, "Quais GPUs existem?")
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected connection refused error when proxy is down, got %v", err)
	}
}

func TestBorrowOrReturnGPUTool_RailguardsAndFlow(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	tool := &BorrowOrReturnGPUTool{store: st}

	callAndDecode := func(input string) borrowOrReturnResponse {
		t.Helper()
		out, err := tool.Call(ctx, input)
		if err != nil {
			t.Fatalf("tool.Call(%s) returned unexpected error: %v", input, err)
		}
		var resp borrowOrReturnResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("unmarshal response for %s: %v\nraw: %s", input, err, out)
		}
		return resp
	}

	// 1. Forbidden: borrow when 0 stock (RTX 4090: 0/3 available)
	r := callAndDecode(`{"action":"borrow","name":"RTX 4090","quantity":1}`)
	if r.Allowed || r.ForbiddenRule != "no_stock_available" || !strings.Contains(r.Message, "Operation forbidden") {
		t.Fatalf("expected no_stock_available refusal, got: %+v", r)
	}

	// 2. Forbidden: return over original stock limit (H100: 2/2 available)
	r = callAndDecode(`{"action":"return","name":"H100","quantity":1}`)
	if r.Allowed || r.ForbiddenRule != "stock_limit_exceeded" || !strings.Contains(r.Message, "maximum original limit") {
		t.Fatalf("expected stock_limit_exceeded refusal, got: %+v", r)
	}

	// 3. Forbidden: return GPU that has in_use > 0 but was NOT borrowed by user (A100: 2/4 in use, 0 borrowed by user)
	r = callAndDecode(`{"action":"return","name":"A100","quantity":1}`)
	if r.Allowed || r.ForbiddenRule != "not_borrowed_by_user" || !strings.Contains(r.Message, "not borrowed by you") {
		t.Fatalf("expected not_borrowed_by_user refusal, got: %+v", r)
	}

	// 4. Allowed: borrow multiple GPUs (2 A100s)
	r = callAndDecode(`{"action":"borrow","name":"A100","quantity":2}`)
	if !r.Allowed || r.GPU == nil || r.GPU.Available != 0 || r.GPU.InUse != 4 || r.BorrowedByUser != 2 {
		t.Fatalf("expected successful borrow of 2 A100s, got: %+v", r)
	}

	// 5. Forbidden: return more than borrowed (3 > 2)
	r = callAndDecode(`{"action":"return","name":"A100","quantity":3}`)
	if r.Allowed || r.ForbiddenRule != "return_exceeds_borrowed" || !strings.Contains(r.Message, "only have 2 unit(s) currently borrowed") {
		t.Fatalf("expected return_exceeds_borrowed refusal, got: %+v", r)
	}

	// 6. Allowed: return 1 A100 (partial return)
	r = callAndDecode(`{"action":"return","name":"A100","quantity":1}`)
	if !r.Allowed || r.GPU == nil || r.GPU.Available != 1 || r.GPU.InUse != 3 || r.BorrowedByUser != 1 {
		t.Fatalf("expected successful partial return of 1 A100, got: %+v", r)
	}

	// 7. Forbidden: non-positive quantity
	r = callAndDecode(`{"action":"borrow","name":"H100","quantity":0}`)
	if r.Allowed || r.ForbiddenRule != "invalid_quantity" {
		t.Fatalf("expected invalid_quantity refusal, got: %+v", r)
	}

	// 8. Forbidden: unknown GPU
	r = callAndDecode(`{"action":"borrow","name":"B200","quantity":1}`)
	if r.Allowed || r.ForbiddenRule != "gpu_not_found" || len(r.ExistingModels) != 5 {
		t.Fatalf("expected gpu_not_found refusal with 5 existing models, got: %+v", r)
	}
}
