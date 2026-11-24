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

## 2. Desktop Framework: Wails v3 (`github.com/wailsapp/wails/v3`)
- **Decision**: Upgrade from Wails v2 to Wails v3 (`v3.1.0`).
- **Rationale**:
  - **Modern Linux GUI Toolchain (GTK4 + WebKitGTK 6.0)**: Wails v2 depends on legacy `gtk+-3.0` and `webkit2gtk-4.0`/`webkit2gtk-4.1` packages which are deprecated or absent on modern rolling Linux distributions (such as Arch Linux). Wails v3 targets modern GTK4 and WebKitGTK 6.0 by default while maintaining backward compatibility with GTK3 runners via `-tags gtk3`.
  - **Zero External CLI Requirement**: Wails v3 applications build and run directly via standard Go commands (`go build .` and `go run .`) without requiring a separate global Wails CLI binary.
  - **Embedded Assets & IPC**: Supports native service bindings (`application.NewService`), asset file servers (`application.AssetFileServerFS`), and clean OS lifecycle hooks.

---

## 3. Typed Tools with Transactional Railguards vs. Raw Text-to-SQL
- **Decision**: Implement 5 typed `langchaingo` tools — 4 read-only query tools (`list_gpus`, `get_gpu`, `check_availability`, `find_gpus`) plus 1 guarded mutation tool (`borrow_or_return_gpu`) — querying structured SQLite repositories through safe parameterized methods, instead of allowing raw Text-to-SQL generation.
- **Rationale**:
  - **Zero SQL Injection / Arbitrary Mutation**: The LLM cannot execute `DROP`, arbitrary `UPDATE`/`INSERT`, or unconstrained table queries.
  - **Defense-in-Depth Railguards**: `borrow_or_return_gpu` enforces password authorization (`GPU_LOAN_PASSWORD`, validated inside the SQLite store in constant time so the LLM never sees the env secret), stock availability checks, original `total_quantity` upper-bound checks, and user loan ownership (`gpu_loans` table) inside a single SQLite transaction.
  - **Deterministic Filtering**: Numerical ranges (e.g., `min_memory_gb`, `only_available`) and boolean MIG capabilities are validated and executed deterministically.
  - **Reliable Tool Calling on Small Models**: Compact local models like `qwen2.5:3b` / `qwen3.5:2b` handle typed function calling schemas (`tools.Tool`) far more reliably than generating dialect-specific SQL syntax.

---

## 4. Pure-Go SQLite Driver (`modernc.org/sqlite`)
- **Decision**: Use `modernc.org/sqlite` instead of `mattn/go-sqlite3`.
- **Rationale**:
  - Pure Go implementation that requires no CGO (`CGO_ENABLED=0` compatible for the database layer).
  - Eliminates native C toolchain / cross-compilation headaches across environments while maintaining full SQLite transactional semantics and compatibility with Wails Linux desktop builds.

---

## 5. Deterministic Simulated Lab GPU Inventory
- **Decision**: Seed an idempotent catalog of 5 enterprise NVIDIA GPUs with fixed, deterministic `in_use` counts:
  | ID | Name | Architecture | VRAM (GB) | MIG Support | Quantity | In Use | Available | Rationale |
  | --- | --- | --- | --- | --- | --- | --- | --- | --- |
  | `a100-80gb` | NVIDIA A100 | Ampere | 80 | Yes | 4 | 2 | 2 | Partial utilization (50%) for testing available queries |
  | `l40s-48gb` | NVIDIA L40S | Ada Lovelace | 48 | No | 6 | 1 | 5 | High availability workstation/inference GPU |
  | `rtx4090-24gb` | NVIDIA RTX 4090 | Ada Lovelace | 24 | No | 3 | 3 | 0 | Fully occupied (100% in-use) to test zero-availability responses |
  | `h100-80gb` | NVIDIA H100 | Hopper | 80 | Yes | 2 | 0 | 2 | Fully available (0% in-use) top-tier LLM training accelerator |
  | `a40-48gb` | NVIDIA A40 | Ampere | 48 | No | 4 | 4 | 0 | Fully occupied (100% in-use) virtualization accelerator |
- **Rationale**: Preserves the exact 5 models, total quantities (19), and VRAM specs from the challenge table while simulating realistic `in_use` utilization so the assistant can demonstrate distinguishing total `quantity` from simulated `in_use` and `available = quantity - in_use`, as well as transactional borrow/return flows.

---

## 6. Dual Runtime Wails + Browser Dev Bridge
- **Decision**: Provide standard native Wails desktop application support while also enabling an HTTP server mode (`--http-addr` / `HTTP_ADDR`) with embedded static frontend and REST endpoints.
- **Rationale**: Allows rapid frontend development, browser debugging, and automated headless integration testing (including automated responsive tests) without needing an active display server or X11/Wayland session.

---

## 7. Pluggable Model Architecture & Google Gemini Free Tier Integration
- **Decision**: Decouple model provisioning and vendor protocols completely from the Go application via LiteLLM, supporting local Ollama (`qwen2.5:3b`, `llama3.2`, etc.) and Google Gemini Free Tier API (`gemini/gemini-2.5-flash` via `GEMINI_API_KEY`) as first-class, drop-in options with automatic local failover.
- **Rationale**:
  - **Zero Go Recompilation**: Evaluators or end users can switch between local CPU/GPU Ollama and high-performance cloud LLMs (Google Gemini 2.5 Flash, Groq, OpenAI) purely by modifying `.env`.
  - **High Performance on Free Tier**: Google AI Studio provides free access to `gemini-2.5-flash` with substantial rate limits, allowing near-instantaneous agent response and tool-calling execution for users without local GPU acceleration.
  - **Guaranteed Resiliency via Fallbacks**: LiteLLM's `router_settings.fallbacks` automatically reroutes chat completion requests to `gpu-assistant-local` (local Ollama) if any cloud upstream experiences an HTTP 429 rate limit or network partition.
  - **Containerized Secret Injection**: `deploy/docker-compose.yml` safely maps provider keys (`GEMINI_API_KEY`, `OPENAI_API_KEY`, `GROQ_API_KEY`) into the proxy container while keeping `.env` excluded from version control.

---

## 8. Current Limitations & Future Work
- **Simulated Availability & Single-Tenant Loan Password**: `in_use` counts are simulated locally in SQLite rather than pulled from live cluster telemetry (e.g. Slurm/Kubernetes/DCGM), and `borrow_or_return_gpu` uses a single shared `GPU_LOAN_PASSWORD` rather than per-user RBAC/OAuth.
- **Observability**: Per-session timestamped trace logs (`app/logs/agent-*.log`, with automatic password redaction) are implemented for local debugging and demos; production deployments would export structured OpenTelemetry traces.
- **Local Model Context Limit**: Compact 2B–4B models operate comfortably within 4k–8k token context windows; conversation compaction or summarization can be introduced if conversation histories extend beyond 50 turns.
