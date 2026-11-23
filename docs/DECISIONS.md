# Architectural & Engineering Decisions

This document records the architectural decisions, trade-offs, and system rationale for the GPU Assistant desktop application.

---

## 1. Single Model Alias (`gpu-assistant`) via LiteLLM
- **Decision**: Configure a single model alias `gpu-assistant` in `deploy/litellm/config.yaml` backed by a local Ollama model (`${OLLAMA_MODEL:-qwen2.5:3b}`) rather than a multi-model router or dual-agent pipeline.
- **Rationale**:
  - The entire application architecture is simple, transparent, and explainable in under 5 minutes during evaluation.
  - Avoids brittle intent routing heuristics or multi-turn handoffs between models.
  - Decouples the application code (`app/`) completely from the underlying LLM provider: swapping between local Ollama (`qwen2.5:3b`, `qwen3:4b`, `llama3.2`) or hosted APIs (OpenAI, Anthropic) requires only changing `deploy/litellm/config.yaml` or `.env` without modifying a single line of Go.

---

## 2. Typed Read-Only Tools vs. Raw Text-to-SQL
- **Decision**: Implement 4 typed, read-only `langchaingo` tools (`list_gpus`, `get_gpu`, `check_availability`, `find_gpus`) that query structured SQLite repositories through safe parameterized methods, instead of allowing raw Text-to-SQL generation.
- **Rationale**:
  - **Zero SQL Injection / Mutation**: The LLM cannot execute `DROP`, `UPDATE`, `INSERT`, or unconstrained table queries.
  - **Deterministic Filtering**: Numerical ranges (e.g., `min_vram_gb`, `only_available`) and boolean MIG capabilities are validated and executed deterministically.
  - **Reliable Tool Calling on Small Models**: Compact local models like `qwen2.5:3b` handle typed function calling schemas (`tools.Tool`) far more reliably than generating dialect-specific SQL syntax.

---

## 3. Pure-Go SQLite Driver (`modernc.org/sqlite`)
- **Decision**: Use `modernc.org/sqlite` instead of `mattn/go-sqlite3`.
- **Rationale**:
  - Pure Go implementation that requires no CGO (`CGO_ENABLED=0` compatible for the database layer).
  - Eliminates native C toolchain / cross-compilation headaches across Linux environments while maintaining full SQLite transactional semantics and compatibility with Wails Linux desktop builds.

---

## 4. Deterministic Simulated Lab GPU Inventory
- **Decision**: Seed an idempotent catalog of 5 enterprise NVIDIA GPUs with fixed, deterministic `in_use` counts:
  | ID | Name | Architecture | VRAM (GB) | MIG Support | Quantity | In Use | Available | Rationale |
  | --- | --- | --- | --- | --- | --- | --- | --- | --- |
  | `a100-80gb` | NVIDIA A100 | Ampere | 80 | Yes | 4 | 2 | 2 | Partial utilization (50%) for testing available queries |
  | `l40s-48gb` | NVIDIA L40S | Ada Lovelace | 48 | No | 6 | 1 | 5 | High availability workstation/inference GPU |
  | `rtx4090-24gb` | NVIDIA RTX 4090 | Ada Lovelace | 24 | No | 3 | 3 | 0 | Fully occupied (100% in-use) to test zero-availability responses |
  | `h100-80gb` | NVIDIA H100 | Hopper | 80 | Yes | 2 | 0 | 2 | Fully available (0% in-use) top-tier LLM training accelerator |
  | `a40-48gb` | NVIDIA A40 | Ampere | 48 | No | 4 | 4 | 0 | Fully occupied (100% in-use) virtualization accelerator |
- **Rationale**: Provides clear contrast between available and exhausted hardware, testing the model's accuracy in distinguishing total `quantity` from current `in_use` and `available = quantity - in_use`.

---

## 5. Dual Runtime Wails + Browser Dev Bridge
- **Decision**: Provide standard native Wails IPC bindings (`window.go.main.App`) while also enabling a fallback REST bridge when running in a standalone browser.
- **Rationale**: Allows rapid frontend development via Vite hot-reloading (`localhost:5173`) and automated headless browser responsive testing without needing a display server or X11/Wayland desktop session.

---

## 6. Current Limitations & Future Work
- **Static In-Use Counts**: GPU allocation is currently read-only and deterministic; future iterations could add authenticated `reserve_gpu` and `release_gpu` mutations with user session leasing.
- **Local Model Context Limit**: Compact 3B/4B models operate comfortably within 4k-8k token context windows; conversation compaction or summarization can be introduced if conversation histories extend beyond 50 turns.
