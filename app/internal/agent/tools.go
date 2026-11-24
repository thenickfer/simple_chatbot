package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/tools"

	"gpu-assistant/app/internal/store"
)

// Compile-time checks that all 5 tools implement langchaingo/tools.Tool.
var (
	_ tools.Tool = (*ListGPUsTool)(nil)
	_ tools.Tool = (*GetGPUTool)(nil)
	_ tools.Tool = (*CheckAvailabilityTool)(nil)
	_ tools.Tool = (*FindGPUsTool)(nil)
	_ tools.Tool = (*BorrowOrReturnGPUTool)(nil)
)

// NewTools returns the 5 typed GPU tools backed by the SQLite Store.
func NewTools(st *store.Store) []tools.Tool {
	return []tools.Tool{
		&ListGPUsTool{store: st},
		&GetGPUTool{store: st},
		&CheckAvailabilityTool{store: st},
		&FindGPUsTool{store: st},
		&BorrowOrReturnGPUTool{store: st},
	}
}

// LLMToolDefinitions returns the OpenAI/LiteLLM function-calling definitions for the 4 tools.
func LLMToolDefinitions() []llms.Tool {
	return []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "list_gpus",
				Description: "List all GPUs in the infrastructure with memory_gb, total_quantity, in_use, available, architecture, mig_support, and summary metrics (including which GPU has the most memory per GPU and most aggregate available VRAM).",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "get_gpu",
				Description: "Get detailed specifications and current availability for a single GPU model by name or ID (e.g. 'A100', 'L40S', 'RTX 4090', 'H100', 'A40', 'B200'). Returns an explicit not-found status if the GPU is not in the infrastructure.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "GPU model name or query (e.g. 'A100', 'H100', 'B200')",
						},
					},
					"required": []string{"name"},
				},
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "check_availability",
				Description: "Check whether a specific quantity of a GPU model is currently available (available = total_quantity - in_use).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "GPU model name (e.g. 'A100', 'L40S', 'RTX 4090', 'H100', 'A40')",
						},
						"quantity": map[string]any{
							"type":        "integer",
							"description": "Requested number of GPU units (default 1)",
						},
					},
					"required": []string{"name"},
				},
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "find_gpus",
				Description: "Find GPUs matching filter criteria such as minimum VRAM (min_memory_gb) or minimum available units (min_available / only_available), sorted by memory_gb DESC, available DESC.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"min_memory_gb": map[string]any{
							"type":        "integer",
							"description": "Minimum memory in GB per GPU (e.g. 48 or 80)",
						},
						"min_available": map[string]any{
							"type":        "integer",
							"description": "Minimum number of currently available units",
						},
						"only_available": map[string]any{
							"type":        "boolean",
							"description": "If true, only include GPUs with at least 1 available unit",
						},
						"architecture": map[string]any{
							"type":        "string",
							"description": "Optional architecture filter (e.g. 'Ampere', 'Ada Lovelace', 'Hopper')",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "borrow_or_return_gpu",
				Description: "Borrow or return GPU units in the infrastructure stock. Borrowing decreases available stock (increases in_use) and records the user's loan. Returning increases available stock (decreases in_use). Enforces railguards: cannot borrow when stock is 0 or insufficient; cannot return above the original total_quantity limit; can only return GPUs previously borrowed by the user.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"enum":        []string{"borrow", "return"},
							"description": "Operation to perform: 'borrow' to check out GPUs or 'return' to give back GPUs you previously borrowed",
						},
						"name": map[string]any{
							"type":        "string",
							"description": "GPU model name (e.g. 'A100', 'L40S', 'RTX 4090', 'H100', 'A40')",
						},
						"quantity": map[string]any{
							"type":        "integer",
							"description": "Number of GPU units to borrow or return (positive integer, default 1)",
						},
					},
					"required": []string{"action", "name"},
				},
			},
		},
	}
}

// 1. list_gpus

type ListGPUsTool struct {
	store *store.Store
}

func (t *ListGPUsTool) Name() string {
	return "list_gpus"
}

func (t *ListGPUsTool) Description() string {
	return "Lists all GPUs in the infrastructure with name, memory_gb, total_quantity, in_use, available, architecture, mig_support, and memory summary metrics."
}

type listGPUsSummary struct {
	TotalModels                    int      `json:"total_models"`
	TotalUnitsInstalled            int      `json:"total_units_installed"`
	TotalUnitsInUse                int      `json:"total_units_in_use"`
	TotalUnitsAvailable            int      `json:"total_units_available"`
	MaxPerGPUMemoryGB              int      `json:"max_per_gpu_memory_gb"`
	HighestPerGPUMemoryModels      []string `json:"highest_per_gpu_memory_models"`
	HighestAvailablePerGPUModels   []string `json:"highest_available_per_gpu_models"`
	HighestInstalledTotalMemoryGPU string   `json:"highest_installed_total_memory_gpu"`
	HighestAvailableAggregateGPU   string   `json:"highest_available_aggregate_vram_gpu"`
}

type listGPUsResponse struct {
	GPUs          []store.GPU     `json:"gpus"`
	Summary       listGPUsSummary `json:"summary"`
	MarkdownTable string          `json:"markdown_table"`
}

func (t *ListGPUsTool) Call(ctx context.Context, _ string) (string, error) {
	gpus, err := t.store.ListGPUs(ctx)
	if err != nil {
		return "", err
	}

	var (
		totalInstalled int
		totalInUse     int
		totalAvail     int
		maxPerGPU      int
		maxInstalledGB int
		maxInstalledNm string
		maxAvailPoolGB int
		maxAvailPoolNm string
	)

	for _, g := range gpus {
		totalInstalled += g.TotalQuantity
		totalInUse += g.InUse
		totalAvail += g.Available
		if g.MemoryGB > maxPerGPU {
			maxPerGPU = g.MemoryGB
		}
		if g.TotalMemoryGB > maxInstalledGB {
			maxInstalledGB = g.TotalMemoryGB
			maxInstalledNm = fmt.Sprintf("%s (%d x %d GB = %d GB total installed, %d x %d GB = %d GB available)", g.Name, g.TotalQuantity, g.MemoryGB, g.TotalMemoryGB, g.Available, g.MemoryGB, g.AvailableTotalMemoryGB)
		}
		if g.AvailableTotalMemoryGB > maxAvailPoolGB {
			maxAvailPoolGB = g.AvailableTotalMemoryGB
			maxAvailPoolNm = fmt.Sprintf("%s (%d available x %d GB = %d GB aggregate available VRAM)", g.Name, g.Available, g.MemoryGB, g.AvailableTotalMemoryGB)
		}
	}

	var highestPerGPU []string
	var highestAvailPerGPU []string
	for _, g := range gpus {
		if g.MemoryGB == maxPerGPU {
			highestPerGPU = append(highestPerGPU, fmt.Sprintf("%s (%d GB per GPU, %d/%d available = %d GB available)", g.Name, g.MemoryGB, g.Available, g.TotalQuantity, g.AvailableTotalMemoryGB))
			if g.Available > 0 {
				highestAvailPerGPU = append(highestAvailPerGPU, fmt.Sprintf("%s (%d GB per GPU, %d available, %d GB aggregate available)", g.Name, g.MemoryGB, g.Available, g.AvailableTotalMemoryGB))
			}
		}
	}

	var md strings.Builder
	md.WriteString("| GPU | Architecture | VRAM (GB) | Total | In Use | Available | Available VRAM (GB) | MIG |\n")
	md.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, g := range gpus {
		mig := "No"
		if g.MIGSupport {
			mig = "Yes"
		}
		md.WriteString(fmt.Sprintf("| %s | %s | %d GB | %d | %d | %d | %d GB | %s |\n",
			g.Name, g.Architecture, g.MemoryGB, g.TotalQuantity, g.InUse, g.Available, g.AvailableTotalMemoryGB, mig))
	}

	resp := listGPUsResponse{
		GPUs: gpus,
		Summary: listGPUsSummary{
			TotalModels:                    len(gpus),
			TotalUnitsInstalled:            totalInstalled,
			TotalUnitsInUse:                totalInUse,
			TotalUnitsAvailable:            totalAvail,
			MaxPerGPUMemoryGB:              maxPerGPU,
			HighestPerGPUMemoryModels:      highestPerGPU,
			HighestAvailablePerGPUModels:   highestAvailPerGPU,
			HighestInstalledTotalMemoryGPU: maxInstalledNm,
			HighestAvailableAggregateGPU:   maxAvailPoolNm,
		},
		MarkdownTable: md.String(),
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// 2. get_gpu

type GetGPUTool struct {
	store *store.Store
}

func (t *GetGPUTool) Name() string {
	return "get_gpu"
}

func (t *GetGPUTool) Description() string {
	return "Looks up a single GPU model by name or JSON {\"name\": \"...\"} with fuzzy matching. Returns full specs and availability or an explicit 'GPU not found in infrastructure' message."
}

type getGPUResponse struct {
	Found          bool       `json:"found"`
	Query          string     `json:"query"`
	Message        string     `json:"message,omitempty"`
	GPU            *store.GPU `json:"gpu,omitempty"`
	ExistingModels []string   `json:"existing_models,omitempty"`
}

func (t *GetGPUTool) Call(ctx context.Context, input string) (string, error) {
	query := parseNameInput(input)
	gpu, err := t.store.GetGPU(ctx, query)
	if err != nil {
		if errors.Is(err, store.ErrGPUNotFound) {
			all, _ := t.store.ListGPUs(ctx)
			names := make([]string, 0, len(all))
			for _, g := range all {
				names = append(names, fmt.Sprintf("%s (%d GB, %d/%d available)", g.Name, g.MemoryGB, g.Available, g.TotalQuantity))
			}
			resp := getGPUResponse{
				Found:          false,
				Query:          query,
				Message:        fmt.Sprintf("GPU not found in infrastructure: %q is not present in the catalog.", query),
				ExistingModels: names,
			}
			b, _ := json.MarshalIndent(resp, "", "  ")
			return string(b), nil
		}
		return "", err
	}

	resp := getGPUResponse{
		Found:   true,
		Query:   query,
		Message: fmt.Sprintf("%s found: %d GB VRAM (%s), %d of %d available (%d in use).", gpu.Name, gpu.MemoryGB, gpu.Architecture, gpu.Available, gpu.TotalQuantity, gpu.InUse),
		GPU:     gpu,
	}
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// 3. check_availability

type CheckAvailabilityTool struct {
	store *store.Store
}

func (t *CheckAvailabilityTool) Name() string {
	return "check_availability"
}

func (t *CheckAvailabilityTool) Description() string {
	return "Checks if a requested quantity of a GPU model is currently available. Accepts JSON {\"name\": \"A100\", \"quantity\": 2} or \"A100, 2\"."
}

type checkAvailabilityResponse struct {
	Found          bool     `json:"found"`
	GPUName        string   `json:"gpu_name,omitempty"`
	Query          string   `json:"query"`
	Requested      int      `json:"requested"`
	Available      bool     `json:"available"`
	AvailableCount int      `json:"available_count"`
	InUse          int      `json:"in_use"`
	TotalQuantity  int      `json:"total_quantity"`
	MemoryGB       int      `json:"memory_gb,omitempty"`
	Architecture   string   `json:"architecture,omitempty"`
	MIGSupport     bool     `json:"mig_support,omitempty"`
	Message        string   `json:"message"`
	ExistingModels []string `json:"existing_models,omitempty"`
}

func (t *CheckAvailabilityTool) Call(ctx context.Context, input string) (string, error) {
	name, qty := parseAvailabilityInput(input)
	if qty <= 0 {
		qty = 1
	}

	gpu, err := t.store.GetGPU(ctx, name)
	if err != nil {
		if errors.Is(err, store.ErrGPUNotFound) {
			all, _ := t.store.ListGPUs(ctx)
			names := make([]string, 0, len(all))
			for _, g := range all {
				names = append(names, fmt.Sprintf("%s (%d/%d available)", g.Name, g.Available, g.TotalQuantity))
			}
			resp := checkAvailabilityResponse{
				Found:          false,
				Query:          name,
				Requested:      qty,
				Available:      false,
				Message:        fmt.Sprintf("GPU not found in infrastructure: %q does not exist in our catalog.", name),
				ExistingModels: names,
			}
			b, _ := json.MarshalIndent(resp, "", "  ")
			return string(b), nil
		}
		return "", err
	}

	isAvail := gpu.Available >= qty
	var msg string
	if isAvail {
		msg = fmt.Sprintf("Yes: %d %s requested and %d of %d units are currently available (%d in use, %d GB VRAM each).",
			qty, gpu.Name, gpu.Available, gpu.TotalQuantity, gpu.InUse, gpu.MemoryGB)
	} else {
		msg = fmt.Sprintf("No: %d %s requested, but only %d of %d units are currently available (%d in use, %d GB VRAM each).",
			qty, gpu.Name, gpu.Available, gpu.TotalQuantity, gpu.InUse, gpu.MemoryGB)
	}

	resp := checkAvailabilityResponse{
		Found:          true,
		GPUName:        gpu.Name,
		Query:          name,
		Requested:      qty,
		Available:      isAvail,
		AvailableCount: gpu.Available,
		InUse:          gpu.InUse,
		TotalQuantity:  gpu.TotalQuantity,
		MemoryGB:       gpu.MemoryGB,
		Architecture:   gpu.Architecture,
		MIGSupport:     gpu.MIGSupport,
		Message:        msg,
	}
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// 4. find_gpus

type FindGPUsTool struct {
	store *store.Store
}

func (t *FindGPUsTool) Name() string {
	return "find_gpus"
}

func (t *FindGPUsTool) Description() string {
	return "Filters GPUs by min_memory_gb, min_available, only_available, or architecture, returning matching GPUs sorted by memory_gb DESC, available DESC."
}

type findGPUsResponse struct {
	Filter  store.GPUFilter `json:"filter"`
	Count   int             `json:"count"`
	GPUs    []store.GPU     `json:"gpus"`
	Message string          `json:"message"`
}

func (t *FindGPUsTool) Call(ctx context.Context, input string) (string, error) {
	filter := parseFindFilterInput(input)
	matched, err := t.store.FindGPUs(ctx, filter)
	if err != nil {
		return "", err
	}

	msg := fmt.Sprintf("Found %d GPU model(s) matching filter (min_memory_gb=%d, min_available=%d, only_available=%v).",
		len(matched), filter.MinMemoryGB, filter.MinAvailable, filter.OnlyAvailable)

	resp := findGPUsResponse{
		Filter:  filter,
		Count:   len(matched),
		GPUs:    matched,
		Message: msg,
	}
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// 5. borrow_or_return_gpu

type BorrowOrReturnGPUTool struct {
	store *store.Store
}

func (t *BorrowOrReturnGPUTool) Name() string {
	return "borrow_or_return_gpu"
}

func (t *BorrowOrReturnGPUTool) Description() string {
	return "Borrows or returns GPU units, updating available stock and enforcing stock and user-ownership railguards. Accepts JSON {\"action\": \"borrow\"|\"return\", \"name\": \"A100\", \"quantity\": 1}."
}

type borrowOrReturnResponse struct {
	Allowed        bool       `json:"allowed"`
	Action         string     `json:"action"`
	GPUName        string     `json:"gpu_name,omitempty"`
	Query          string     `json:"query"`
	Quantity       int        `json:"quantity"`
	ForbiddenRule  string     `json:"forbidden_rule,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	Message        string     `json:"message"`
	GPU            *store.GPU `json:"gpu,omitempty"`
	BorrowedByUser int        `json:"borrowed_by_user"`
	ExistingModels []string   `json:"existing_models,omitempty"`
}

func (t *BorrowOrReturnGPUTool) Call(ctx context.Context, input string) (string, error) {
	action, name, qty, qtyExplicit := parseLoanInput(input)
	if !qtyExplicit {
		qty = 1
	}

	marshalResp := func(r borrowOrReturnResponse) (string, error) {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	if action != "borrow" && action != "return" {
		reason := fmt.Sprintf("invalid action %q: must be 'borrow' or 'return'", action)
		return marshalResp(borrowOrReturnResponse{
			Allowed:       false,
			Action:        action,
			Query:         name,
			Quantity:      qty,
			ForbiddenRule: "invalid_action",
			Reason:        reason,
			Message:       "Operation forbidden: " + reason + ".",
		})
	}

	if qty <= 0 {
		reason := fmt.Sprintf("quantity must be a positive integer (at least 1), got %d", qty)
		return marshalResp(borrowOrReturnResponse{
			Allowed:       false,
			Action:        action,
			Query:         name,
			Quantity:      qty,
			ForbiddenRule: "invalid_quantity",
			Reason:        reason,
			Message:       "Operation forbidden: " + reason + ".",
		})
	}

	var (
		gpu *store.GPU
		err error
	)
	if action == "borrow" {
		gpu, err = t.store.BorrowGPU(ctx, name, qty)
	} else {
		gpu, err = t.store.ReturnGPU(ctx, name, qty)
	}

	if err != nil {
		if errors.Is(err, store.ErrGPUNotFound) {
			all, _ := t.store.ListGPUs(ctx)
			names := make([]string, 0, len(all))
			for _, g := range all {
				names = append(names, fmt.Sprintf("%s (%d/%d available, %d borrowed by you)", g.Name, g.Available, g.TotalQuantity, g.BorrowedByUser))
			}
			reason := fmt.Sprintf("GPU not found in infrastructure: %q does not exist in our catalog.", name)
			return marshalResp(borrowOrReturnResponse{
				Allowed:        false,
				Action:         action,
				Query:          name,
				Quantity:       qty,
				ForbiddenRule:  "gpu_not_found",
				Reason:         reason,
				Message:        "Operation forbidden: " + reason,
				ExistingModels: names,
			})
		}

		rule := classifyLoanGuardrailError(err)
		if rule != "" {
			resp := borrowOrReturnResponse{
				Allowed:       false,
				Action:        action,
				Query:         name,
				Quantity:      qty,
				ForbiddenRule: rule,
				Reason:        err.Error(),
				Message:       "Operation forbidden: " + err.Error(),
				GPU:           gpu,
			}
			if gpu != nil {
				resp.GPUName = gpu.Name
				resp.BorrowedByUser = gpu.BorrowedByUser
			}
			return marshalResp(resp)
		}

		return "", err
	}

	var msg string
	if action == "borrow" {
		msg = fmt.Sprintf("Borrowed %d %s. Stock updated: %d/%d available (%d in use, %d currently borrowed by you).",
			qty, gpu.Name, gpu.Available, gpu.TotalQuantity, gpu.InUse, gpu.BorrowedByUser)
	} else {
		msg = fmt.Sprintf("Returned %d %s. Stock updated: %d/%d available (%d in use, %d currently borrowed by you).",
			qty, gpu.Name, gpu.Available, gpu.TotalQuantity, gpu.InUse, gpu.BorrowedByUser)
	}

	return marshalResp(borrowOrReturnResponse{
		Allowed:        true,
		Action:         action,
		GPUName:        gpu.Name,
		Query:          name,
		Quantity:       qty,
		Message:        msg,
		GPU:            gpu,
		BorrowedByUser: gpu.BorrowedByUser,
	})
}

func classifyLoanGuardrailError(err error) string {
	switch {
	case errors.Is(err, store.ErrInvalidQuantity):
		return "invalid_quantity"
	case errors.Is(err, store.ErrNoStock):
		return "no_stock_available"
	case errors.Is(err, store.ErrInsufficientStock):
		return "insufficient_stock"
	case errors.Is(err, store.ErrStockLimitExceeded):
		return "stock_limit_exceeded"
	case errors.Is(err, store.ErrNotBorrowedByUser):
		return "not_borrowed_by_user"
	case errors.Is(err, store.ErrReturnExceedsBorrowed):
		return "return_exceeds_borrowed"
	default:
		return ""
	}
}

func normalizeLoanAction(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "borrow", "checkout", "check_out", "reserve", "take", "rent", "loan", "acquire", "empréstimo", "emprestar", "pegar":
		return "borrow"
	case "return", "release", "give_back", "giveback", "checkin", "check_in", "devolver", "retornar":
		return "return"
	default:
		return s
	}
}

func parseLoanInput(input string) (action, name string, qty int, qtyExplicit bool) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", "", 1, false
	}
	if strings.HasPrefix(raw, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			for _, k := range []string{"action", "operation", "op", "type"} {
				if v, ok := payload[k]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						action = normalizeLoanAction(s)
						break
					}
				}
			}
			for _, k := range []string{"name", "gpu", "model", "query", "id"} {
				if v, ok := payload[k]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						name = strings.TrimSpace(s)
						break
					}
				}
			}
			for _, k := range []string{"quantity", "count", "units", "amount"} {
				if v, ok := payload[k]; ok && v != nil {
					switch val := v.(type) {
					case float64:
						qty = int(val)
						qtyExplicit = true
					case int:
						qty = val
						qtyExplicit = true
					case string:
						if parsed, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
							qty = parsed
							qtyExplicit = true
						}
					}
					if qtyExplicit {
						break
					}
				}
			}
			return action, name, qty, qtyExplicit
		}
	}

	// Plain fallback format: "borrow, A100, 2" or "return A100"
	parts := strings.Split(raw, ",")
	if len(parts) >= 2 {
		action = normalizeLoanAction(strings.Trim(parts[0], `"' `))
		name = strings.Trim(parts[1], `"' `)
		if len(parts) >= 3 {
			if parsed, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
				qty = parsed
				qtyExplicit = true
			}
		}
		return action, name, qty, qtyExplicit
	}

	fields := strings.Fields(raw)
	if len(fields) >= 2 {
		action = normalizeLoanAction(fields[0])
		if parsed, err := strconv.Atoi(fields[len(fields)-1]); err == nil && len(fields) >= 3 {
			qty = parsed
			qtyExplicit = true
			name = strings.Join(fields[1:len(fields)-1], " ")
		} else {
			name = strings.Join(fields[1:], " ")
		}
	}
	return action, name, qty, qtyExplicit
}

// Input parsing helpers for flexible JSON or plain-string tool calls.

func parseNameInput(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			for _, k := range []string{"name", "gpu", "model", "query", "id", "input"} {
				if v, ok := payload[k]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s)
					}
				}
			}
		}
	}
	return strings.Trim(raw, `"'`)
}

func parseAvailabilityInput(input string) (string, int) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", 1
	}
	if strings.HasPrefix(raw, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			var name string
			for _, k := range []string{"name", "gpu", "model", "query", "id"} {
				if v, ok := payload[k]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						name = strings.TrimSpace(s)
						break
					}
				}
			}
			qty := 1
			for _, k := range []string{"quantity", "count", "requested", "min_available", "units"} {
				if v, ok := payload[k]; ok {
					switch val := v.(type) {
					case float64:
						qty = int(val)
					case int:
						qty = val
					case string:
						if parsed, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
							qty = parsed
						}
					}
				}
			}
			return name, qty
		}
	}

	// Plain format: "A100, 2" or "A100:2"
	for _, sep := range []string{",", ":"} {
		if strings.Contains(raw, sep) {
			parts := strings.SplitN(raw, sep, 2)
			name := strings.Trim(strings.TrimSpace(parts[0]), `"'`)
			qty := 1
			if parsed, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && parsed > 0 {
				qty = parsed
			}
			return name, qty
		}
	}

	return strings.Trim(raw, `"'`), 1
}

func parseFindFilterInput(input string) store.GPUFilter {
	var filter store.GPUFilter
	raw := strings.TrimSpace(input)
	if raw == "" || raw == "{}" {
		return filter
	}
	if strings.HasPrefix(raw, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			for _, k := range []string{"min_memory_gb", "min_vram_gb", "memory_gb", "vram_gb"} {
				if v, ok := payload[k]; ok {
					switch val := v.(type) {
					case float64:
						filter.MinMemoryGB = int(val)
					case string:
						if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
							filter.MinMemoryGB = n
						}
					}
				}
			}
			for _, k := range []string{"min_available", "available", "quantity"} {
				if v, ok := payload[k]; ok {
					switch val := v.(type) {
					case float64:
						filter.MinAvailable = int(val)
					case string:
						if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
							filter.MinAvailable = n
						}
					}
				}
			}
			if v, ok := payload["only_available"]; ok {
				if b, ok := v.(bool); ok {
					filter.OnlyAvailable = b
				}
			}
			if v, ok := payload["architecture"]; ok {
				if s, ok := v.(string); ok {
					filter.Architecture = strings.TrimSpace(s)
				}
			}
			for _, k := range []string{"mig_support", "mig_capable", "mig"} {
				if v, ok := payload[k]; ok {
					if b, ok := v.(bool); ok {
						filter.MIGSupport = &b
					}
				}
			}
			return filter
		}
	}

	// If plain number passed, treat as min_memory_gb
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		filter.MinMemoryGB = n
	}
	return filter
}
