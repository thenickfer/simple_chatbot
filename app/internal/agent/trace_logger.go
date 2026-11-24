package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultLogDir is the default directory for timestamped agent trace logs.
	DefaultLogDir = "logs"
	logTimeLayout = "2006-01-02T15:04:05.000Z07:00"
	fileTimeStamp = "20060102-150405"
)

// TraceLogger writes timestamped, human-readable debugging and demo logs of
// user messages, model thoughts/reasoning, tool calls, tool results, and replies.
// All methods are safe to call on a nil receiver.
type TraceLogger struct {
	mu      sync.Mutex
	w       io.Writer
	closer  io.Closer
	path    string
	turnSeq atomic.Uint64
}

// NewTraceLogger creates a TraceLogger writing to w (useful for tests or custom sinks).
func NewTraceLogger(w io.Writer, path string) *TraceLogger {
	if w == nil {
		return nil
	}
	return &TraceLogger{
		w:    w,
		path: path,
	}
}

// NewTraceLoggerFromEnv opens a timestamped log file in AGENT_LOG_DIR (default "./logs")
// or at the explicit AGENT_LOG_PATH. Set AGENT_LOG_DISABLED=true to disable file logging.
func NewTraceLoggerFromEnv(model, baseURL string) (*TraceLogger, error) {
	disabled := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_LOG_DISABLED")))
	if disabled == "true" || disabled == "1" || disabled == "yes" {
		return nil, nil
	}

	logPath := strings.TrimSpace(os.Getenv("AGENT_LOG_PATH"))
	if logPath == "" {
		logDir := strings.TrimSpace(os.Getenv("AGENT_LOG_DIR"))
		if logDir == "" {
			logDir = DefaultLogDir
		}
		filename := fmt.Sprintf("agent-%s.log", time.Now().Format(fileTimeStamp))
		logPath = filepath.Join(logDir, filename)
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open trace log %q: %w", logPath, err)
	}

	absPath, err := filepath.Abs(logPath)
	if err != nil {
		absPath = logPath
	}

	tl := &TraceLogger{
		w:      f,
		closer: f,
		path:   absPath,
	}
	tl.LogSessionStart(model, baseURL)
	return tl, nil
}

// Path returns the filesystem path of the log file (empty if writing to a non-file sink).
func (l *TraceLogger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close closes the underlying log file if owned by the TraceLogger.
func (l *TraceLogger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.closer.Close()
	l.closer = nil
	return err
}

// NextTurn increments and returns the 1-based conversation turn counter for this session.
func (l *TraceLogger) NextTurn() uint64 {
	if l == nil {
		return 0
	}
	return l.turnSeq.Add(1)
}

func (l *TraceLogger) writef(format string, args ...any) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, format, args...)
}

func nowTS() string {
	return time.Now().Format(logTimeLayout)
}

// LogSessionStart records session metadata when the app starts.
func (l *TraceLogger) LogSessionStart(model, baseURL string) {
	l.writef("[%s] [SESSION] started | model=%s base_url=%s\n", nowTS(), model, baseURL)
}

// LogEvent records a general lifecycle event (e.g. conversation cleared).
func (l *TraceLogger) LogEvent(event, detail string) {
	if detail != "" {
		l.writef("[%s] [EVENT] %s | %s\n", nowTS(), event, detail)
		return
	}
	l.writef("[%s] [EVENT] %s\n", nowTS(), event)
}

// LogUserMessage records an incoming user message at the start of a turn.
func (l *TraceLogger) LogUserMessage(turn uint64, text string) {
	l.writef("\n================================================================================\n")
	l.writef("[%s] [TURN %d] [USER]\n%s\n\n", nowTS(), turn, strings.TrimSpace(text))
}

// LogLLMRequest records an outbound call to the LLM.
func (l *TraceLogger) LogLLMRequest(turn uint64, iter, maxIter, msgCount, toolCount int, phase string) {
	if phase == "" {
		phase = "chat"
	}
	l.writef("[%s] [TURN %d] [LLM_REQUEST] phase=%s iter=%d/%d messages=%d tools=%d\n",
		nowTS(), turn, phase, iter, maxIter, msgCount, toolCount)
}

// LogLLMResponse records metadata about a completed LLM response.
func (l *TraceLogger) LogLLMResponse(turn uint64, iter, maxIter int, dur time.Duration, stopReason string, toolCallCount int) {
	if stopReason == "" {
		stopReason = "none"
	}
	l.writef("[%s] [TURN %d] [LLM_RESPONSE] iter=%d/%d duration=%dms stop_reason=%s tool_calls=%d\n",
		nowTS(), turn, iter, maxIter, dur.Milliseconds(), stopReason, toolCallCount)
}

// LogThought records model reasoning/thought text along with where it was extracted from.
func (l *TraceLogger) LogThought(turn uint64, source, thought string) {
	trimmed := strings.TrimSpace(thought)
	if trimmed == "" {
		return
	}
	l.writef("[%s] [TURN %d] [THOUGHT] (source=%s)\n%s\n\n", nowTS(), turn, source, trimmed)
}

// LogToolCall records a tool invocation requested by the model.
func (l *TraceLogger) LogToolCall(turn uint64, callID, toolName, args string) {
	trimmedArgs := strings.TrimSpace(args)
	if trimmedArgs == "" {
		trimmedArgs = "{}"
	}
	l.writef("[%s] [TURN %d] [TOOL_CALL] id=%s name=%s\nargs: %s\n\n", nowTS(), turn, callID, toolName, trimmedArgs)
}

// LogToolResult records the output (or error) returned by a tool execution.
func (l *TraceLogger) LogToolResult(turn uint64, callID, toolName string, dur time.Duration, output string, err error) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	l.writef("[%s] [TURN %d] [TOOL_RESULT] id=%s name=%s status=%s duration=%dms\n%s\n\n",
		nowTS(), turn, callID, toolName, status, dur.Milliseconds(), strings.TrimSpace(output))
}

// LogAssistantMessage records the final assistant reply for the turn.
func (l *TraceLogger) LogAssistantMessage(turn uint64, totalDur time.Duration, toolsCalled []string, reply string) {
	toolsStr := "none"
	if len(toolsCalled) > 0 {
		toolsStr = "[" + strings.Join(toolsCalled, ", ") + "]"
	}
	l.writef("[%s] [TURN %d] [ASSISTANT] total_duration=%dms tools_called=%s\n%s\n",
		nowTS(), turn, totalDur.Milliseconds(), toolsStr, strings.TrimSpace(reply))
}

// LogError records an error encountered during a turn.
func (l *TraceLogger) LogError(turn uint64, stage string, err error) {
	if err == nil {
		return
	}
	l.writef("[%s] [TURN %d] [ERROR] stage=%s error=%v\n", nowTS(), turn, stage, err)
}

// extractThinkTags extracts any <think>...</think> blocks from content (common with Qwen3/DeepSeek models).
func extractThinkTags(content string) []string {
	var thoughts []string
	remaining := content
	for {
		lower := strings.ToLower(remaining)
		start := strings.Index(lower, "<think>")
		if start == -1 {
			break
		}
		afterStart := remaining[start+len("<think>"):]
		lowerAfter := strings.ToLower(afterStart)
		end := strings.Index(lowerAfter, "</think>")
		if end == -1 {
			if t := strings.TrimSpace(afterStart); t != "" {
				thoughts = append(thoughts, t)
			}
			break
		}
		if t := strings.TrimSpace(afterStart[:end]); t != "" {
			thoughts = append(thoughts, t)
		}
		remaining = afterStart[end+len("</think>"):]
	}
	return thoughts
}
