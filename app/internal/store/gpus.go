package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors for GPU lookup and borrow/return railguards.
var (
	ErrGPUNotFound               = errors.New("GPU not found in infrastructure")
	ErrLoanPasswordNotConfigured = errors.New("GPU loan password is not configured on the server")
	ErrLoanPasswordRequired      = errors.New("loan password is required to borrow or return GPUs")
	ErrInvalidLoanPassword       = errors.New("invalid loan password")
	ErrInvalidQuantity           = errors.New("quantity must be a positive integer")
	ErrNoStock                   = errors.New("no available GPU stock to borrow")
	ErrInsufficientStock         = errors.New("insufficient available GPU stock")
	ErrStockLimitExceeded        = errors.New("cannot return GPU above original stock limit")
	ErrNotBorrowedByUser         = errors.New("GPU was not borrowed by the user")
	ErrReturnExceedsBorrowed     = errors.New("return quantity exceeds units borrowed by the user")
)

// GPU represents a GPU model in the infrastructure inventory.
type GPU struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Architecture           string `json:"architecture"`
	MemoryGB               int    `json:"memory_gb"`
	MIGSupport             bool   `json:"mig_support"`
	TotalQuantity          int    `json:"total_quantity"`
	InUse                  int    `json:"in_use"`
	Available              int    `json:"available"`
	BorrowedByUser         int    `json:"borrowed_by_user"`
	TotalMemoryGB          int    `json:"total_memory_gb"`
	AvailableTotalMemoryGB int    `json:"available_total_memory_gb"`
	Notes                  string `json:"notes"`
}

// GPUFilter defines structured filter criteria for FindGPUs.
type GPUFilter struct {
	MinMemoryGB   int    `json:"min_memory_gb"`
	MinAvailable  int    `json:"min_available"`
	OnlyAvailable bool   `json:"only_available"`
	Architecture  string `json:"architecture"`
	MIGSupport    *bool  `json:"mig_support,omitempty"`
}

var defaultGPUSeed = []GPU{
	{
		ID:            "a100-80gb",
		Name:          "NVIDIA A100",
		Architecture:  "Ampere",
		MemoryGB:      80,
		MIGSupport:    true,
		TotalQuantity: 4,
		InUse:         2,
		Notes:         "SXM4, NVLink, MIG (up to 7x 10GB instances), double-precision & large-scale AI training/inference",
	},
	{
		ID:            "l40s-48gb",
		Name:          "NVIDIA L40S",
		Architecture:  "Ada Lovelace",
		MemoryGB:      48,
		MIGSupport:    false,
		TotalQuantity: 6,
		InUse:         1,
		Notes:         "PCIe Gen4, 4th-gen Tensor Cores, FP8 inference, graphics & generative AI workloads",
	},
	{
		ID:            "rtx4090-24gb",
		Name:          "NVIDIA RTX 4090",
		Architecture:  "Ada Lovelace",
		MemoryGB:      24,
		MIGSupport:    false,
		TotalQuantity: 3,
		InUse:         3,
		Notes:         "PCIe Gen4, high clock speed, workstation prototyping & fine-tuning",
	},
	{
		ID:            "h100-80gb",
		Name:          "NVIDIA H100",
		Architecture:  "Hopper",
		MemoryGB:      80,
		MIGSupport:    true,
		TotalQuantity: 2,
		InUse:         0,
		Notes:         "SXM5, Transformer Engine (FP8), MIG (up to 7x 10GB instances), highest throughput LLM training",
	},
	{
		ID:            "a40-48gb",
		Name:          "NVIDIA A40",
		Architecture:  "Ampere",
		MemoryGB:      48,
		MIGSupport:    false,
		TotalQuantity: 4,
		InUse:         4,
		Notes:         "PCIe Gen4, passive cooling, virtual workstation, rendering & inference",
	},
}

// SeedGPUs idempotently inserts or updates static metadata for the 5 required NVIDIA GPUs
// without clobbering live in_use counts on existing databases.
func (s *Store) SeedGPUs(ctx context.Context) error {
	const upsertSQL = `
INSERT INTO gpus (id, name, architecture, memory_gb, mig_support, total_quantity, in_use, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	name = excluded.name,
	architecture = excluded.architecture,
	memory_gb = excluded.memory_gb,
	mig_support = excluded.mig_support,
	total_quantity = excluded.total_quantity,
	notes = excluded.notes;
`
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, g := range defaultGPUSeed {
		if _, err := stmt.ExecContext(
			ctx,
			g.ID,
			g.Name,
			g.Architecture,
			g.MemoryGB,
			g.MIGSupport,
			g.TotalQuantity,
			g.InUse,
			g.Notes,
		); err != nil {
			return fmt.Errorf("seed gpu %s: %w", g.Name, err)
		}
	}

	return tx.Commit()
}

func hydrateGPU(g *GPU) {
	avail := g.TotalQuantity - g.InUse
	if avail < 0 {
		avail = 0
	}
	g.Available = avail
	g.TotalMemoryGB = g.TotalQuantity * g.MemoryGB
	g.AvailableTotalMemoryGB = g.Available * g.MemoryGB
}

// ListGPUs returns all GPUs in deterministic order.
func (s *Store) ListGPUs(ctx context.Context) ([]GPU, error) {
	const query = `
SELECT g.id, g.name, g.architecture, g.memory_gb, g.mig_support, g.total_quantity, g.in_use, COALESCE(l.borrowed_by_user, 0), g.notes
FROM gpus g
LEFT JOIN gpu_loans l ON l.gpu_id = g.id
ORDER BY CASE g.id
	WHEN 'a100-80gb' THEN 1
	WHEN 'l40s-48gb' THEN 2
	WHEN 'rtx4090-24gb' THEN 3
	WHEN 'h100-80gb' THEN 4
	WHEN 'a40-48gb' THEN 5
	ELSE 99
END, g.name ASC;
`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GPU
	for rows.Next() {
		var g GPU
		if err := rows.Scan(
			&g.ID,
			&g.Name,
			&g.Architecture,
			&g.MemoryGB,
			&g.MIGSupport,
			&g.TotalQuantity,
			&g.InUse,
			&g.BorrowedByUser,
			&g.Notes,
		); err != nil {
			return nil, err
		}
		hydrateGPU(&g)
		result = append(result, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// normalizeGPUQuery strips vendor prefixes and non-alphanumeric separators for fuzzy matching.
func normalizeGPUQuery(q string) string {
	s := strings.ToLower(strings.TrimSpace(q))
	s = strings.TrimPrefix(s, "nvidia")
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// GetGPU performs a fuzzy, case-insensitive lookup for a GPU by name, ID, or model number.
// Matches inputs like "A100", "a100", "nvidia a100", "4090", "rtx 4090", "l40s", "h100", "a40".
// Returns ErrGPUNotFound for unknown models such as "B200".
func (s *Store) GetGPU(ctx context.Context, query string) (*GPU, error) {
	raw := strings.TrimSpace(query)
	if raw == "" {
		return nil, ErrGPUNotFound
	}
	normQuery := normalizeGPUQuery(raw)
	if normQuery == "" {
		return nil, ErrGPUNotFound
	}

	gpus, err := s.ListGPUs(ctx)
	if err != nil {
		return nil, err
	}

	// Pass 1: exact ID, exact name (case-insensitive), or exact normalized model name.
	for i := range gpus {
		g := &gpus[i]
		if strings.EqualFold(g.ID, raw) || strings.EqualFold(g.Name, raw) {
			return g, nil
		}
		normName := normalizeGPUQuery(g.Name)
		normID := normalizeGPUQuery(g.ID)
		if normQuery == normName || normQuery == normID {
			return g, nil
		}
		// Match "4090" against "rtx4090"
		if strings.TrimPrefix(normName, "rtx") == normQuery {
			return g, nil
		}
	}

	// Pass 2: token or prefix/suffix match on normalized model identifier (minimum 3 chars, e.g. "a40", "l40", "h100", "a100", "4090").
	if len(normQuery) >= 3 {
		for i := range gpus {
			g := &gpus[i]
			normName := normalizeGPUQuery(g.Name)
			normID := normalizeGPUQuery(g.ID)
			if strings.Contains(normName, normQuery) || strings.Contains(normQuery, normName) || strings.HasPrefix(normID, normQuery) {
				return g, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrGPUNotFound, raw)
}

// FindGPUs filters GPUs by memory, availability, architecture, and MIG support,
// returning matching GPUs sorted by memory_gb DESC, available DESC, name ASC.
func (s *Store) FindGPUs(ctx context.Context, filter GPUFilter) ([]GPU, error) {
	all, err := s.ListGPUs(ctx)
	if err != nil {
		return nil, err
	}

	minAvail := filter.MinAvailable
	if filter.OnlyAvailable && minAvail < 1 {
		minAvail = 1
	}
	archFilter := strings.ToLower(strings.TrimSpace(filter.Architecture))

	var matched []GPU
	for _, g := range all {
		if filter.MinMemoryGB > 0 && g.MemoryGB < filter.MinMemoryGB {
			continue
		}
		if minAvail > 0 && g.Available < minAvail {
			continue
		}
		if archFilter != "" && !strings.Contains(strings.ToLower(g.Architecture), archFilter) {
			continue
		}
		if filter.MIGSupport != nil && g.MIGSupport != *filter.MIGSupport {
			continue
		}
		matched = append(matched, g)
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].MemoryGB != matched[j].MemoryGB {
			return matched[i].MemoryGB > matched[j].MemoryGB
		}
		if matched[i].Available != matched[j].Available {
			return matched[i].Available > matched[j].Available
		}
		return matched[i].Name < matched[j].Name
	})

	return matched, nil
}

func (s *Store) verifyLoanPassword(provided string) error {
	expected := strings.TrimSpace(s.loanPassword)
	if expected == "" {
		return fmt.Errorf("%w: set GPU_LOAN_PASSWORD in .env to enable borrow/return operations", ErrLoanPasswordNotConfigured)
	}
	candidate := strings.TrimSpace(provided)
	if candidate == "" {
		return fmt.Errorf("%w: please provide the loan password to authorize this operation", ErrLoanPasswordRequired)
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) != 1 {
		return fmt.Errorf("%w: the provided password does not match the configured GPU loan password", ErrInvalidLoanPassword)
	}
	return nil
}

// BorrowGPU decreases available stock (increases in_use) and records the borrowed units
// for the user inside a single SQLite transaction.
// Enforces railguards:
// - loan password must match GPU_LOAN_PASSWORD configured in the store
// - quantity must be >= 1
// - GPU must exist
// - stock must be available (available > 0 and available >= quantity)
func (s *Store) BorrowGPU(ctx context.Context, query string, quantity int, password string) (*GPU, error) {
	if err := s.verifyLoanPassword(password); err != nil {
		return nil, err
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidQuantity, quantity)
	}

	target, err := s.GetGPU(ctx, query)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var g GPU
	err = tx.QueryRowContext(ctx, `
SELECT g.id, g.name, g.architecture, g.memory_gb, g.mig_support, g.total_quantity, g.in_use, COALESCE(l.borrowed_by_user, 0), g.notes
FROM gpus g
LEFT JOIN gpu_loans l ON l.gpu_id = g.id
WHERE g.id = ?;`, target.ID).Scan(
		&g.ID,
		&g.Name,
		&g.Architecture,
		&g.MemoryGB,
		&g.MIGSupport,
		&g.TotalQuantity,
		&g.InUse,
		&g.BorrowedByUser,
		&g.Notes,
	)
	if err != nil {
		return nil, err
	}
	hydrateGPU(&g)

	if g.Available <= 0 {
		return &g, fmt.Errorf("%w: cannot borrow %d %s because there is no available stock (0/%d available, all %d units are currently in use)",
			ErrNoStock, quantity, g.Name, g.TotalQuantity, g.InUse)
	}
	if quantity > g.Available {
		return &g, fmt.Errorf("%w: cannot borrow %d %s because only %d/%d units are currently available (%d in use)",
			ErrInsufficientStock, quantity, g.Name, g.Available, g.TotalQuantity, g.InUse)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE gpus SET in_use = in_use + ? WHERE id = ?;`, quantity, g.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO gpu_loans (gpu_id, borrowed_by_user, updated_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(gpu_id) DO UPDATE SET
	borrowed_by_user = borrowed_by_user + excluded.borrowed_by_user,
	updated_at = CURRENT_TIMESTAMP;`, g.ID, quantity); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	g.InUse += quantity
	g.BorrowedByUser += quantity
	hydrateGPU(&g)
	return &g, nil
}

// ReturnGPU increases available stock (decreases in_use) and decrements the user's
// borrowed count inside a single SQLite transaction.
// Enforces railguards:
// - loan password must match GPU_LOAN_PASSWORD configured in the store
// - quantity must be >= 1
// - GPU must exist
// - stock cannot be returned over the original total_quantity limit (in_use cannot drop below 0)
// - GPUs can only be returned if they were previously borrowed by the user (borrowed_by_user >= quantity)
func (s *Store) ReturnGPU(ctx context.Context, query string, quantity int, password string) (*GPU, error) {
	if err := s.verifyLoanPassword(password); err != nil {
		return nil, err
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidQuantity, quantity)
	}

	target, err := s.GetGPU(ctx, query)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var g GPU
	err = tx.QueryRowContext(ctx, `
SELECT g.id, g.name, g.architecture, g.memory_gb, g.mig_support, g.total_quantity, g.in_use, COALESCE(l.borrowed_by_user, 0), g.notes
FROM gpus g
LEFT JOIN gpu_loans l ON l.gpu_id = g.id
WHERE g.id = ?;`, target.ID).Scan(
		&g.ID,
		&g.Name,
		&g.Architecture,
		&g.MemoryGB,
		&g.MIGSupport,
		&g.TotalQuantity,
		&g.InUse,
		&g.BorrowedByUser,
		&g.Notes,
	)
	if err != nil {
		return nil, err
	}
	hydrateGPU(&g)

	if g.InUse <= 0 || g.Available >= g.TotalQuantity {
		return &g, fmt.Errorf("%w: %s stock is already at its maximum original limit (%d/%d available, 0 in use)",
			ErrStockLimitExceeded, g.Name, g.Available, g.TotalQuantity)
	}
	if g.BorrowedByUser <= 0 {
		return &g, fmt.Errorf("%w: you have 0 %s units borrowed (the %d unit(s) currently in use were not borrowed by you and cannot be returned)",
			ErrNotBorrowedByUser, g.Name, g.InUse)
	}
	if quantity > g.BorrowedByUser {
		return &g, fmt.Errorf("%w: cannot return %d %s because you only have %d unit(s) currently borrowed",
			ErrReturnExceedsBorrowed, quantity, g.Name, g.BorrowedByUser)
	}
	if g.InUse-quantity < 0 || g.Available+quantity > g.TotalQuantity {
		return &g, fmt.Errorf("%w: returning %d %s would exceed the original stock limit of %d units (currently %d/%d available)",
			ErrStockLimitExceeded, quantity, g.Name, g.TotalQuantity, g.Available, g.TotalQuantity)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE gpus SET in_use = in_use - ? WHERE id = ?;`, quantity, g.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE gpu_loans
SET borrowed_by_user = borrowed_by_user - ?, updated_at = CURRENT_TIMESTAMP
WHERE gpu_id = ?;`, quantity, g.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	g.InUse -= quantity
	g.BorrowedByUser -= quantity
	hydrateGPU(&g)
	return &g, nil
}
