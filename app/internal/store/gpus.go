package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrGPUNotFound is returned when a requested GPU model does not exist in the catalog.
var ErrGPUNotFound = errors.New("GPU not found in infrastructure")

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

// SeedGPUs idempotently inserts or updates the 5 required NVIDIA GPUs.
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
	in_use = excluded.in_use,
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
SELECT id, name, architecture, memory_gb, mig_support, total_quantity, in_use, notes
FROM gpus
ORDER BY CASE id
	WHEN 'a100-80gb' THEN 1
	WHEN 'l40s-48gb' THEN 2
	WHEN 'rtx4090-24gb' THEN 3
	WHEN 'h100-80gb' THEN 4
	WHEN 'a40-48gb' THEN 5
	ELSE 99
END, name ASC;
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
