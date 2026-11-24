package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/tools"

	"gpu-assistant/app/internal/store"
)

const MaxToolIterations = 4

// LLMClient abstracts a langchaingo chat model so unit tests can inject a deterministic
// fake LLM while production uses the LiteLLM OpenAI-compatible client.
type LLMClient interface {
	GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error)
}

// Response represents the final output of an agent chat turn.
type Response struct {
	Reply     string              `json:"reply"`
	ToolCalls []string            `json:"tool_calls,omitempty"`
	Message   store.StoredMessage `json:"message"`
}

// Agent coordinates conversation history, SQLite GPU tools, and the single configured LLM.
type Agent struct {
	store    *store.Store
	llm      LLMClient
	toolList []tools.Tool
	toolMap  map[string]tools.Tool
	llmTools []llms.Tool
	logger   *TraceLogger
}

// SetTraceLogger attaches a TraceLogger for debugging/demo logging of messages, thoughts, and tool calls.
func (a *Agent) SetTraceLogger(logger *TraceLogger) {
	a.logger = logger
}

// New creates an Agent backed by the given SQLite store and LLMClient.
func New(st *store.Store, client LLMClient) *Agent {
	tList := NewTools(st)
	tMap := make(map[string]tools.Tool, len(tList))
	for _, t := range tList {
		tMap[t.Name()] = t
	}
	return &Agent{
		store:    st,
		llm:      client,
		toolList: tList,
		toolMap:  tMap,
		llmTools: LLMToolDefinitions(),
	}
}

// Tools returns the registered langchaingo tools.
func (a *Agent) Tools() []tools.Tool {
	return a.toolList
}

// Chat processes a user question, persists the user turn, executes a bounded tool loop
// (up to MaxToolIterations = 4) with resilient fallback parsing for local models, persists
// the assistant response, and returns the result.
func (a *Agent) Chat(ctx context.Context, userText string) (Response, error) {
	trimmed := strings.TrimSpace(userText)
	if trimmed == "" {
		return Response{}, errors.New("message cannot be empty")
	}
	if a.llm == nil {
		return Response{}, errors.New("LLM client is not configured")
	}

	// Load prior conversation history before recording the new user message.
	historyMsgs, err := a.store.History().Messages(ctx)
	if err != nil {
		return Response{}, fmt.Errorf("load conversation history: %w", err)
	}

	// Build message sequence starting with the strict anti-hallucination system prompt.
	convo := make([]llms.MessageContent, 0, len(historyMsgs)+4)
	convo = append(convo, llms.TextParts(llms.ChatMessageTypeSystem, SystemPrompt))

	// Include recent conversation turns (cap to last 12 messages to keep local model context lean).
	startIdx := 0
	if len(historyMsgs) > 12 {
		startIdx = len(historyMsgs) - 12
	}
	for _, m := range historyMsgs[startIdx:] {
		convo = append(convo, llms.TextParts(m.GetType(), m.GetContent()))
	}

	// Append current user turn.
	convo = append(convo, llms.TextParts(llms.ChatMessageTypeHuman, trimmed))

	turn := a.logger.NextTurn()
	turnStart := time.Now()
	a.logger.LogUserMessage(turn, trimmed)

	var invokedTools []string
	var finalReply string

	for iter := 0; iter < MaxToolIterations; iter++ {
		a.logger.LogLLMRequest(turn, iter+1, MaxToolIterations, len(convo), len(a.llmTools), "chat")
		llmStart := time.Now()
		resp, err := a.llm.GenerateContent(
			ctx,
			convo,
			llms.WithTools(a.llmTools),
			llms.WithTemperature(0.0),
		)
		llmDur := time.Since(llmStart)
		if err != nil {
			a.logger.LogError(turn, "llm_request", err)
			return Response{}, fmt.Errorf("LiteLLM request failed: %w", err)
		}
		if resp == nil || len(resp.Choices) == 0 {
			emptyErr := errors.New("empty response from LiteLLM model")
			a.logger.LogError(turn, "llm_response", emptyErr)
			return Response{}, emptyErr
		}

		choice := resp.Choices[0]
		toolCalls := extractToolCalls(choice, a.toolMap)
		a.logger.LogLLMResponse(turn, iter+1, MaxToolIterations, llmDur, choice.StopReason, len(toolCalls))
		a.logChoiceThoughts(turn, choice, len(toolCalls) > 0)

		if len(toolCalls) == 0 {
			finalReply = strings.TrimSpace(choice.Content)
			break
		}

		// Record assistant tool-call request in the conversation trace.
		assistantParts := make([]llms.ContentPart, 0, len(toolCalls)+1)
		if strings.TrimSpace(choice.Content) != "" && !looksLikeRawJSONToolCall(choice.Content) {
			assistantParts = append(assistantParts, llms.TextPart(choice.Content))
		}
		for _, tc := range toolCalls {
			assistantParts = append(assistantParts, tc)
		}
		convo = append(convo, llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: assistantParts,
		})

		// Execute each requested tool against the SQLite store.
		for idx, tc := range toolCalls {
			if tc.FunctionCall == nil {
				continue
			}
			toolName := strings.TrimSpace(tc.FunctionCall.Name)
			toolArgs := tc.FunctionCall.Arguments
			callID := tc.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%d_%d", iter, idx)
			}

			a.logger.LogToolCall(turn, callID, toolName, toolArgs)
			toolStart := time.Now()
			toolImpl, ok := a.toolMap[toolName]
			var toolOutput string
			var toolErr error
			if !ok {
				toolErr = fmt.Errorf("unknown tool %q", toolName)
				toolOutput = fmt.Sprintf(`{"error": "unknown tool %q"}`, toolName)
			} else {
				invokedTools = append(invokedTools, toolName)
				out, callErr := toolImpl.Call(ctx, toolArgs)
				if callErr != nil {
					toolErr = callErr
					toolOutput = fmt.Sprintf(`{"error": %q}`, callErr.Error())
				} else {
					toolOutput = out
				}
			}
			a.logger.LogToolResult(turn, callID, toolName, time.Since(toolStart), toolOutput, toolErr)

			convo = append(convo, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: callID,
						Name:       toolName,
						Content:    toolOutput,
					},
				},
			})
		}
	}

	// If the model hit MaxToolIterations while still calling tools, ask for a final synthesis without tools.
	if finalReply == "" {
		a.logger.LogLLMRequest(turn, MaxToolIterations+1, MaxToolIterations, len(convo), 0, "synthesis")
		synthStart := time.Now()
		resp, err := a.llm.GenerateContent(
			ctx,
			convo,
			llms.WithTemperature(0.0),
		)
		synthDur := time.Since(synthStart)
		if err != nil {
			a.logger.LogError(turn, "llm_synthesis", err)
			return Response{}, fmt.Errorf("LiteLLM synthesis failed: %w", err)
		}
		if resp != nil && len(resp.Choices) > 0 {
			choice := resp.Choices[0]
			a.logger.LogLLMResponse(turn, MaxToolIterations+1, MaxToolIterations, synthDur, choice.StopReason, 0)
			a.logChoiceThoughts(turn, choice, false)
			finalReply = strings.TrimSpace(choice.Content)
		}
	}

	if finalReply == "" {
		emptyErr := errors.New("model returned an empty response")
		a.logger.LogError(turn, "final_reply", emptyErr)
		return Response{}, emptyErr
	}

	// Persist both the user message and the assistant response in SQLite.
	if err := a.store.AddUserMessage(ctx, trimmed); err != nil {
		a.logger.LogError(turn, "save_user_message", err)
		return Response{}, fmt.Errorf("save user message: %w", err)
	}
	savedMsg, err := a.store.AddAIMessageWithTools(ctx, finalReply, invokedTools)
	if err != nil {
		a.logger.LogError(turn, "save_assistant_message", err)
		return Response{}, fmt.Errorf("save assistant message: %w", err)
	}

	a.logger.LogAssistantMessage(turn, time.Since(turnStart), invokedTools, finalReply)

	return Response{
		Reply:     finalReply,
		ToolCalls: invokedTools,
		Message:   savedMsg,
	}, nil
}

func (a *Agent) logChoiceThoughts(turn uint64, choice *llms.ContentChoice, hasToolCalls bool) {
	if a.logger == nil || choice == nil {
		return
	}
	reasoning := strings.TrimSpace(choice.ReasoningContent)
	if reasoning != "" {
		a.logger.LogThought(turn, "reasoning_content", reasoning)
	}
	if choice.GenerationInfo != nil {
		for _, key := range []string{"reasoning_content", "reasoning"} {
			if raw, ok := choice.GenerationInfo[key]; ok {
				if s, ok := raw.(string); ok {
					trimmed := strings.TrimSpace(s)
					if trimmed != "" && trimmed != reasoning {
						a.logger.LogThought(turn, "generation_info."+key, trimmed)
					}
				}
			}
		}
	}
	thinkBlocks := extractThinkTags(choice.Content)
	for _, block := range thinkBlocks {
		if block != reasoning {
			a.logger.LogThought(turn, "think_tag", block)
		}
	}
	if hasToolCalls && len(thinkBlocks) == 0 {
		trimmedContent := strings.TrimSpace(choice.Content)
		if trimmedContent != "" && !looksLikeRawJSONToolCall(trimmedContent) {
			a.logger.LogThought(turn, "assistant_pre_tool", trimmedContent)
		}
	}
}

// extractToolCalls inspects structured ToolCalls, legacy FuncCall, and fallback JSON tool calls
// inside choice.Content (which smaller local Ollama checkpoints occasionally emit).
func extractToolCalls(choice *llms.ContentChoice, knownTools map[string]tools.Tool) []llms.ToolCall {
	if choice == nil {
		return nil
	}
	if len(choice.ToolCalls) > 0 {
		out := make([]llms.ToolCall, 0, len(choice.ToolCalls))
		for i, tc := range choice.ToolCalls {
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("toolcall_%d", i+1)
			}
			if tc.Type == "" {
				tc.Type = "function"
			}
			out = append(out, tc)
		}
		return out
	}
	if choice.FuncCall != nil && choice.FuncCall.Name != "" {
		return []llms.ToolCall{
			{
				ID:           "func_1",
				Type:         "function",
				FunctionCall: choice.FuncCall,
			},
		}
	}
	return parseFallbackToolCallsFromContent(choice.Content, knownTools)
}

func looksLikeRawJSONToolCall(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "{") && (strings.Contains(trimmed, `"name"`) || strings.Contains(trimmed, `"tool"`))
}

func parseFallbackToolCallsFromContent(content string, knownTools map[string]tools.Tool) []llms.ToolCall {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	// Strip optional ```json ... ``` fences if present.
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			trimmed = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end <= start {
		return nil
	}
	candidate := trimmed[start : end+1]

	var raw map[string]any
	if err := json.Unmarshal([]byte(candidate), &raw); err != nil {
		return nil
	}

	// Shape 1: {"name": "get_gpu", "arguments": {...}} or {"tool": "get_gpu", "parameters": {...}}
	var fnName string
	for _, k := range []string{"name", "tool", "function"} {
		if v, ok := raw[k]; ok {
			if s, ok := v.(string); ok {
				fnName = strings.TrimSpace(s)
				break
			}
		}
	}

	if fnName != "" {
		if _, exists := knownTools[fnName]; exists {
			argsStr := "{}"
			for _, argKey := range []string{"arguments", "parameters", "args", "input"} {
				if argVal, ok := raw[argKey]; ok && argVal != nil {
					switch v := argVal.(type) {
					case string:
						argsStr = v
					default:
						if b, err := json.Marshal(v); err == nil {
							argsStr = string(b)
						}
					}
					break
				}
			}
			return []llms.ToolCall{
				{
					ID:   "fallback_call_1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      fnName,
						Arguments: argsStr,
					},
				},
			}
		}
	}

	return nil
}
