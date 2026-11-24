package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestStoreMigrateAndIdempotentSeed(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_gpu.db")

	st, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	defer st.Close()

	// Calling SeedGPUs multiple times must remain idempotent with exactly 5 GPUs.
	if err := st.SeedGPUs(ctx); err != nil {
		t.Fatalf("Second SeedGPUs failed: %v", err)
	}

	gpus, err := st.ListGPUs(ctx)
	if err != nil {
		t.Fatalf("ListGPUs failed: %v", err)
	}
	if len(gpus) != 5 {
		t.Fatalf("expected 5 GPUs, got %d", len(gpus))
	}

	expected := []struct {
		Name          string
		MemoryGB      int
		TotalQuantity int
		InUse         int
		Available     int
		Architecture  string
		MIGSupport    bool
	}{
		{"NVIDIA A100", 80, 4, 2, 2, "Ampere", true},
		{"NVIDIA L40S", 48, 6, 1, 5, "Ada Lovelace", false},
		{"NVIDIA RTX 4090", 24, 3, 3, 0, "Ada Lovelace", false},
		{"NVIDIA H100", 80, 2, 0, 2, "Hopper", true},
		{"NVIDIA A40", 48, 4, 4, 0, "Ampere", false},
	}

	for i, exp := range expected {
		got := gpus[i]
		if got.Name != exp.Name {
			t.Errorf("gpu[%d] name = %q, want %q", i, got.Name, exp.Name)
		}
		if got.MemoryGB != exp.MemoryGB {
			t.Errorf("%s memory_gb = %d, want %d", exp.Name, got.MemoryGB, exp.MemoryGB)
		}
		if got.TotalQuantity != exp.TotalQuantity {
			t.Errorf("%s total_quantity = %d, want %d", exp.Name, got.TotalQuantity, exp.TotalQuantity)
		}
		if got.InUse != exp.InUse {
			t.Errorf("%s in_use = %d, want %d", exp.Name, got.InUse, exp.InUse)
		}
		if got.Available != exp.Available {
			t.Errorf("%s available = %d, want %d", exp.Name, got.Available, exp.Available)
		}
		if got.Architecture != exp.Architecture {
			t.Errorf("%s architecture = %q, want %q", exp.Name, got.Architecture, exp.Architecture)
		}
		if got.MIGSupport != exp.MIGSupport {
			t.Errorf("%s mig_support = %v, want %v", exp.Name, got.MIGSupport, exp.MIGSupport)
		}
	}
}

func TestGetGPUFuzzyLookupAndNotFound(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fuzzy.db")

	st, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	defer st.Close()

	cases := []struct {
		query    string
		wantName string
	}{
		{"A100", "NVIDIA A100"},
		{"a100", "NVIDIA A100"},
		{"nvidia a100", "NVIDIA A100"},
		{"NVIDIA A100", "NVIDIA A100"},
		{"a100-80gb", "NVIDIA A100"},
		{"4090", "NVIDIA RTX 4090"},
		{"rtx 4090", "NVIDIA RTX 4090"},
		{"NVIDIA RTX 4090", "NVIDIA RTX 4090"},
		{"l40s", "NVIDIA L40S"},
		{"L40S", "NVIDIA L40S"},
		{"h100", "NVIDIA H100"},
		{"a40", "NVIDIA A40"},
	}

	for _, tc := range cases {
		gpu, err := st.GetGPU(ctx, tc.query)
		if err != nil {
			t.Fatalf("GetGPU(%q) unexpected error: %v", tc.query, err)
		}
		if gpu.Name != tc.wantName {
			t.Errorf("GetGPU(%q) = %q, want %q", tc.query, gpu.Name, tc.wantName)
		}
	}

	for _, missing := range []string{"B200", "nvidia b200", "RTX 5090", "V100", ""} {
		gpu, err := st.GetGPU(ctx, missing)
		if err == nil || !errors.Is(err, ErrGPUNotFound) {
			t.Errorf("GetGPU(%q) expected ErrGPUNotFound, got gpu=%v err=%v", missing, gpu, err)
		}
	}
}

func TestFindGPUsFilterAndSorting(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "find.db")

	st, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	defer st.Close()

	// Filter: min_memory_gb=48, only_available=true -> should match A100 (80GB, 2 avail), H100 (80GB, 2 avail), L40S (48GB, 5 avail).
	// A40 has 48GB but 0 available so it must be excluded.
	res, err := st.FindGPUs(ctx, GPUFilter{
		MinMemoryGB:   48,
		OnlyAvailable: true,
	})
	if err != nil {
		t.Fatalf("FindGPUs failed: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 matching GPUs, got %d", len(res))
	}
	if res[0].MemoryGB != 80 || res[1].MemoryGB != 80 || res[2].Name != "NVIDIA L40S" {
		t.Errorf("unexpected FindGPUs order: %+v", res)
	}
}

func TestChatMessageHistoryPersistenceAcrossReopenAndClear(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "history.db")

	st, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}

	hist := st.History()
	if err := hist.AddUserMessage(ctx, "Quais GPUs existem na infraestrutura?"); err != nil {
		t.Fatalf("AddUserMessage failed: %v", err)
	}
	if err := hist.AddAIMessage(ctx, "Temos 5 modelos de GPU na infraestrutura."); err != nil {
		t.Fatalf("AddAIMessage failed: %v", err)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen same database file and verify history survived restart.
	st2, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Reopen store failed: %v", err)
	}
	defer st2.Close()

	msgs, err := st2.History().Messages(ctx)
	if err != nil {
		t.Fatalf("Messages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 persisted messages after reopen, got %d", len(msgs))
	}
	if msgs[0].GetType() != llms.ChatMessageTypeHuman || msgs[0].GetContent() != "Quais GPUs existem na infraestrutura?" {
		t.Errorf("msg[0] = (%v, %q)", msgs[0].GetType(), msgs[0].GetContent())
	}
	if msgs[1].GetType() != llms.ChatMessageTypeAI || msgs[1].GetContent() != "Temos 5 modelos de GPU na infraestrutura." {
		t.Errorf("msg[1] = (%v, %q)", msgs[1].GetType(), msgs[1].GetContent())
	}

	// Test SetMessages and Clear.
	err = st2.History().SetMessages(ctx, []llms.ChatMessage{
		llms.HumanChatMessage{Content: "Do you have a B200?"},
	})
	if err != nil {
		t.Fatalf("SetMessages failed: %v", err)
	}
	msgs, err = st2.History().Messages(ctx)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("expected 1 message after SetMessages, got %d (err=%v)", len(msgs), err)
	}

	if err := st2.History().Clear(ctx); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	msgs, err = st2.History().Messages(ctx)
	if err != nil {
		t.Fatalf("Messages after Clear failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after Clear, got %d", len(msgs))
	}
}

func TestBorrowAndReturnGPURailguardsAndPersistence(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "loans.db")

	st, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}

	// 1. Borrowing when no stock is available (RTX 4090: 3/3 in use, 0 available) must be forbidden.
	if _, err := st.BorrowGPU(ctx, "RTX 4090", 1); !errors.Is(err, ErrNoStock) {
		t.Fatalf("expected ErrNoStock for RTX 4090, got %v", err)
	}

	// 2. Borrowing more than available (A100: 2 available) must be forbidden.
	if _, err := st.BorrowGPU(ctx, "A100", 3); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock for 3 A100s, got %v", err)
	}

	// 3. Returning when already at full original stock (H100: 2/2 available, 0 in use) must be forbidden.
	if _, err := st.ReturnGPU(ctx, "H100", 1); !errors.Is(err, ErrStockLimitExceeded) {
		t.Fatalf("expected ErrStockLimitExceeded for H100, got %v", err)
	}

	// 4. Returning a GPU that has units in_use (A100: 2/4 in use) but was NOT borrowed by the user must be forbidden.
	if _, err := st.ReturnGPU(ctx, "A100", 1); !errors.Is(err, ErrNotBorrowedByUser) {
		t.Fatalf("expected ErrNotBorrowedByUser for A100, got %v", err)
	}

	// 5. Borrowing multiple GPUs succeeds (2 A100s + 1 L40S).
	gA100, err := st.BorrowGPU(ctx, "A100", 2)
	if err != nil {
		t.Fatalf("BorrowGPU(A100, 2) failed: %v", err)
	}
	if gA100.Available != 0 || gA100.InUse != 4 || gA100.BorrowedByUser != 2 {
		t.Fatalf("unexpected A100 state after borrowing 2: %+v", gA100)
	}

	gL40S, err := st.BorrowGPU(ctx, "L40S", 1)
	if err != nil {
		t.Fatalf("BorrowGPU(L40S, 1) failed: %v", err)
	}
	if gL40S.Available != 4 || gL40S.InUse != 2 || gL40S.BorrowedByUser != 1 {
		t.Fatalf("unexpected L40S state after borrowing 1: %+v", gL40S)
	}

	// 6. Returning more than the user borrowed must be forbidden (even though A100 has 4 in_use).
	if _, err := st.ReturnGPU(ctx, "A100", 3); !errors.Is(err, ErrReturnExceedsBorrowed) {
		t.Fatalf("expected ErrReturnExceedsBorrowed for 3 A100s, got %v", err)
	}

	// 7. Reopening the database (which re-runs SeedGPUs) must preserve live stock and loan counts.
	if err := st.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	st2, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Reopen store failed: %v", err)
	}
	defer st2.Close()

	reopenedA100, err := st2.GetGPU(ctx, "A100")
	if err != nil {
		t.Fatalf("GetGPU(A100) after reopen failed: %v", err)
	}
	if reopenedA100.Available != 0 || reopenedA100.InUse != 4 || reopenedA100.BorrowedByUser != 2 {
		t.Fatalf("stock/loan not preserved across reopen: %+v", reopenedA100)
	}

	// 8. Returning the 2 borrowed A100s succeeds and restores original stock (2/4 available, 0 borrowed).
	returnedA100, err := st2.ReturnGPU(ctx, "A100", 2)
	if err != nil {
		t.Fatalf("ReturnGPU(A100, 2) failed: %v", err)
	}
	if returnedA100.Available != 2 || returnedA100.InUse != 2 || returnedA100.BorrowedByUser != 0 {
		t.Fatalf("unexpected A100 state after return: %+v", returnedA100)
	}

	// 9. Trying to return another A100 after returning all borrowed units must be forbidden.
	if _, err := st2.ReturnGPU(ctx, "A100", 1); !errors.Is(err, ErrNotBorrowedByUser) {
		t.Fatalf("expected ErrNotBorrowedByUser after full return, got %v", err)
	}
}
