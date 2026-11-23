package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/schema"
)

// Compile-time check that *Store and *SQLiteChatMessageHistory implement schema.ChatMessageHistory.
var _ schema.ChatMessageHistory = (*Store)(nil)
var _ schema.ChatMessageHistory = (*SQLiteChatMessageHistory)(nil)

// StoredMessage represents a persisted conversation turn in the messages table.
type StoredMessage struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolCalls []string  `json:"tool_calls,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SQLiteChatMessageHistory wraps *Store to satisfy langchaingo's schema.ChatMessageHistory.
type SQLiteChatMessageHistory struct {
	store *Store
}

// NewChatMessageHistory returns a schema.ChatMessageHistory backed by the Store's messages table.
func NewChatMessageHistory(s *Store) *SQLiteChatMessageHistory {
	return &SQLiteChatMessageHistory{store: s}
}

// History returns a schema.ChatMessageHistory view of the Store.
func (s *Store) History() *SQLiteChatMessageHistory {
	return NewChatMessageHistory(s)
}

// AddMessage adds a langchaingo llms.ChatMessage to the SQLite messages table.
func (s *Store) AddMessage(ctx context.Context, message llms.ChatMessage) error {
	if message == nil {
		return nil
	}
	role := mapChatMessageTypeToRole(message.GetType())
	var toolNames []string
	if aiMsg, ok := message.(llms.AIChatMessage); ok {
		for _, tc := range aiMsg.ToolCalls {
			if tc.FunctionCall != nil && tc.FunctionCall.Name != "" {
				toolNames = append(toolNames, tc.FunctionCall.Name)
			}
		}
		if aiMsg.FunctionCall != nil && aiMsg.FunctionCall.Name != "" && len(toolNames) == 0 {
			toolNames = append(toolNames, aiMsg.FunctionCall.Name)
		}
	}
	_, err := s.InsertStoredMessage(ctx, role, message.GetContent(), toolNames)
	return err
}

// AddUserMessage adds a user/human message to the SQLite messages table.
func (s *Store) AddUserMessage(ctx context.Context, message string) error {
	_, err := s.InsertStoredMessage(ctx, "user", message, nil)
	return err
}

// AddAIMessage adds an AI/assistant message to the SQLite messages table.
func (s *Store) AddAIMessage(ctx context.Context, message string) error {
	_, err := s.InsertStoredMessage(ctx, "assistant", message, nil)
	return err
}

// AddAIMessageWithTools adds an AI/assistant message along with the names of tools invoked.
func (s *Store) AddAIMessageWithTools(ctx context.Context, message string, toolCalls []string) (StoredMessage, error) {
	return s.InsertStoredMessage(ctx, "assistant", message, toolCalls)
}

// InsertStoredMessage inserts a message row and returns the persisted StoredMessage.
func (s *Store) InsertStoredMessage(ctx context.Context, role, content string, toolCalls []string) (StoredMessage, error) {
	role = normalizeRole(role)
	var toolCallsJSON sql.NullString
	if len(toolCalls) > 0 {
		b, err := json.Marshal(toolCalls)
		if err != nil {
			return StoredMessage{}, fmt.Errorf("marshal tool_calls: %w", err)
		}
		toolCallsJSON = sql.NullString{String: string(b), Valid: true}
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`INSERT INTO messages (role, content, tool_calls, created_at) VALUES (?, ?, ?, ?)`,
		role,
		content,
		toolCallsJSON,
		now.Format(time.RFC3339),
	)
	if err != nil {
		return StoredMessage{}, fmt.Errorf("insert message: %w", err)
	}
	id, _ := res.LastInsertId()
	return StoredMessage{
		ID:        id,
		Role:      role,
		Content:   content,
		ToolCalls: toolCalls,
		CreatedAt: now,
	}, nil
}

// Clear removes all messages from the SQLite messages table.
func (s *Store) Clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages`)
	return err
}

// Messages retrieves all messages from the SQLite store as langchaingo llms.ChatMessage values.
func (s *Store) Messages(ctx context.Context) ([]llms.ChatMessage, error) {
	stored, err := s.ListStoredMessages(ctx)
	if err != nil {
		return nil, err
	}
	msgs := make([]llms.ChatMessage, 0, len(stored))
	for _, m := range stored {
		switch normalizeRole(m.Role) {
		case "user", "human":
			msgs = append(msgs, llms.HumanChatMessage{Content: m.Content})
		case "assistant", "ai":
			msgs = append(msgs, llms.AIChatMessage{Content: m.Content})
		case "system":
			msgs = append(msgs, llms.SystemChatMessage{Content: m.Content})
		default:
			msgs = append(msgs, llms.GenericChatMessage{
				Role:    m.Role,
				Content: m.Content,
			})
		}
	}
	return msgs, nil
}

// ListStoredMessages returns all persisted messages ordered by ID ascending.
func (s *Store) ListStoredMessages(ctx context.Context) ([]StoredMessage, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, role, content, tool_calls, created_at FROM messages ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []StoredMessage
	for rows.Next() {
		var (
			m         StoredMessage
			toolCalls sql.NullString
			rawTime   any
		)
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &toolCalls, &rawTime); err != nil {
			return nil, err
		}
		m.Role = normalizeRole(m.Role)
		if toolCalls.Valid && strings.TrimSpace(toolCalls.String) != "" {
			var parsed []string
			if err := json.Unmarshal([]byte(toolCalls.String), &parsed); err == nil {
				m.ToolCalls = parsed
			}
		}
		m.CreatedAt = parseSQLiteTime(rawTime)
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// SetMessages replaces all existing messages in the store with the provided messages.
func (s *Store) SetMessages(ctx context.Context, messages []llms.ChatMessage) error {
	if err := s.Clear(ctx); err != nil {
		return err
	}
	for _, msg := range messages {
		if err := s.AddMessage(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

// Delegate methods on SQLiteChatMessageHistory to Store.

func (h *SQLiteChatMessageHistory) AddMessage(ctx context.Context, message llms.ChatMessage) error {
	return h.store.AddMessage(ctx, message)
}

func (h *SQLiteChatMessageHistory) AddUserMessage(ctx context.Context, message string) error {
	return h.store.AddUserMessage(ctx, message)
}

func (h *SQLiteChatMessageHistory) AddAIMessage(ctx context.Context, message string) error {
	return h.store.AddAIMessage(ctx, message)
}

func (h *SQLiteChatMessageHistory) Clear(ctx context.Context) error {
	return h.store.Clear(ctx)
}

func (h *SQLiteChatMessageHistory) Messages(ctx context.Context) ([]llms.ChatMessage, error) {
	return h.store.Messages(ctx)
}

func (h *SQLiteChatMessageHistory) SetMessages(ctx context.Context, messages []llms.ChatMessage) error {
	return h.store.SetMessages(ctx, messages)
}

func mapChatMessageTypeToRole(t llms.ChatMessageType) string {
	switch t {
	case llms.ChatMessageTypeHuman:
		return "user"
	case llms.ChatMessageTypeAI:
		return "assistant"
	case llms.ChatMessageTypeSystem:
		return "system"
	default:
		return string(t)
	}
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "human", "user":
		return "user"
	case "ai", "assistant":
		return "assistant"
	case "system":
		return "system"
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func parseSQLiteTime(v any) time.Time {
	switch val := v.(type) {
	case time.Time:
		return val.UTC()
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, val); err == nil {
				return t.UTC()
			}
		}
	case []byte:
		return parseSQLiteTime(string(val))
	}
	return time.Now().UTC()
}
