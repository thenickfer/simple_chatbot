# GPU Lab Assistant

> Desktop assistant for querying, inspecting, and filtering research lab GPU infrastructure in real time.  
> Built with **Wails v2 (Go)**, **React + TypeScript**, **LangChainGo**, **LiteLLM**, **Ollama**, and the **Cobalt Lab v2** design system with a 60fps ASCII creature field background.

---

## Architecture Overview / Visão Geral da Arquitetura

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Wails Desktop Application                       │
│                                                                        │
│   ┌────────────────────────────────┐   ┌───────────────────────────┐   │
│   │   Frontend (React + Vite)      │   │     Backend (Go)          │   │
│   │   • Cobalt Lab v2 Dark Design  │IPC│   • Wails App Bindings    │   │
│   │   • 60fps CreatureField Canvas │───│   • LangChainGo Agent     │   │
│   │   • High-Contrast UI Plates    │   │   • 4 Typed SQLite Tools  │   │
│   │   • Markdown + Abbr Semantics  │   │   • modernc.org/sqlite DB │   │
│   └────────────────────────────────┘   └─────────────┬─────────────┘   │
└──────────────────────────────────────────────────────┼─────────────────┘
                                                       │ OpenAI API
                                                       ▼
                                         ┌───────────────────────────┐
                                         │       LiteLLM Proxy       │
                                         │  (deploy/docker-compose)  │
                                         │   Model: gpu-assistant    │
                                         └─────────────┬─────────────┘
                                                       │ Ollama API
                                                       ▼
                                         ┌───────────────────────────┐
                                         │       Ollama Engine       │
                                         │     Local Model (3B)      │
                                         └───────────────────────────┘
```

The architecture is deliberately modular and explainable in 5 minutes:
1. **Frontend**: React single page over a procedural 60fps ASCII `CreatureField` canvas that steers around high-contrast UI plates.
2. **Backend**: Pure-Go agent using `langchaingo` with 4 typed, read-only tools querying an embedded SQLite database (`modernc.org/sqlite`).
3. **LLM Proxy**: Dockerized LiteLLM proxy exposing a standard OpenAI-compatible API to the Go app, routing queries to a local Ollama instance (`qwen2.5:3b` by default).

---

## Simulated GPU Inventory / Inventário de GPUs do Laboratório

The local SQLite database initializes idempotently with 5 enterprise NVIDIA GPUs and deterministic allocation states:

| GPU Model | Architecture | VRAM | MIG Capable | Total Quantity | In Use | Available | Primary Workloads |
| :--- | :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| **NVIDIA A100** | Ampere | 80 GB | Yes (up to 7x) | 4 | 2 | **2** | Large-scale LLM training, FP64 scientific simulations |
| **NVIDIA L40S** | Ada Lovelace | 48 GB | No | 6 | 1 | **5** | Generative AI, FP8 inference, omniverse graphics |
| **NVIDIA RTX 4090** | Ada Lovelace | 24 GB | No | 3 | 3 | **0** | Workstation prototyping, fine-tuning smaller models |
| **NVIDIA H100** | Hopper | 80 GB | Yes (up to 7x) | 2 | 0 | **2** | State-of-the-art Transformer engine training/inference |
| **NVIDIA A40** | Ampere | 48 GB | No | 4 | 4 | **0** | Virtual workstations, graphics rendering, visual computing |

---

## Prerequisites / Pré-requisitos

- **Docker & Docker Compose** (for Ollama and LiteLLM)
- **Go 1.21+**
- **Node.js 18+ & npm**
- **Wails CLI v2**: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Linux desktop build dependencies (if running/compiling on Linux):
  ```bash
  sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev libwebkit2gtk-4.0-dev
  ```

---

## Quick Start / Início Rápido

### 1. Configure Environment
```bash
cp .env.example .env
```

### 2. Start Ollama & LiteLLM Services
```bash
# CPU mode (default):
make up

# Or GPU-accelerated mode (requires NVIDIA Container Toolkit):
make up-gpu
```

### 3. Pull the Local Model
```bash
make models
```
*Pulls `qwen2.5:3b` (or your configured `OLLAMA_MODEL`) into the Ollama container.*

### 4. Run Smoke Test
```bash
make smoke
```
*Verifies that LiteLLM is responsive and the `gpu-assistant` model alias is registered.*

### 5. Launch Application
```bash
# Development mode:
make dev

# Or standalone HTTP server mode directly:
make dev-server

# Production build:
make build
```
*Note: `make dev` automatically detects if the Wails CLI and Linux GTK/WebKit headers are installed. If available, it launches the native desktop window; otherwise, it starts the embedded standalone server at `http://127.0.0.1:34115` with full access to the UI and Go backend.*

---

## Example Test Conversations / Exemplos de Conversas

The assistant natively handles queries in Portuguese and English, strictly grounding all facts in SQLite tool outputs:

### 1. General Catalog Query / Catálogo Geral
> **User**: "Quais GPUs temos no laboratório e quantas estão disponíveis agora?"  
> **Assistant**: Calls `list_gpus`. Formats a clean breakdown of all 5 GPUs with architectures, total quantities, and available units (e.g. A100: 2 disponíveis, L40S: 5 disponíveis, H100: 2 disponíveis, RTX 4090 e A40: esgotadas).

### 2. Workload & VRAM Recommendation / Recomendação por VRAM
> **User**: "Preciso treinar um modelo grande com pelo menos 80GB de VRAM e suporte a MIG. O que você recomenda?"  
> **Assistant**: Calls `find_gpus(min_vram_gb=80, mig_capable=true, only_available=true)`. Explains that both the **NVIDIA H100** (2 disponíveis, 80 GB, Hopper) and **NVIDIA A100** (2 disponíveis, 80 GB, Ampere) meet the criteria, highlighting H100's Transformer Engine.

### 3. Availability Check / Verificação de Disponibilidade
> **User**: "Tem alguma RTX 4090 livre para uso imediato?"  
> **Assistant**: Calls `get_gpu(id_or_name="4090")`. Reports that all 3 RTX 4090 units are currently in use (0 disponíveis) and suggests the available **L40S** (48 GB, Ada Lovelace) as an alternative.

### 4. Anti-Hallucination & Scope Verification / Teste de Não-Alucinação
> **User**: "Vocês têm RTX 3080 ou placas da AMD? E quanto custa a hora de uso?"  
> **Assistant**: Calls `list_gpus`. Clarifies that the lab inventory only contains NVIDIA A100, L40S, RTX 4090, H100, and A40 (no RTX 3080 or AMD GPUs) and states that pricing/billing is not tracked by the system without fabricating rates.

---

## Testing & Quality Assurance / Testes e Qualidade

Run both frontend and backend test suites with:
```bash
make test
```

- **Frontend Tests**: Verifies creature field physics, glyph determinism, viewport area budget (<20k cells), plate avoidance, semantics, and abbreviation rules.
- **Backend Tests**: Verifies SQLite migrations, idempotent seed data, typed tool executions, and conversation history persistence.

---

## Automated Releases / Release Automatizado

Semantic versioning, changelog generation, and desktop binary compilation are automated via GitHub Actions (`.github/workflows/release.yml`):
- Pushes to `main` with Conventional Commits trigger **Release Please** (`.release-please-config.json`, `.release-please-manifest.json`).
- Merging a release PR automatically builds the Wails application, packages `gpu-assistant-${tag}-linux-amd64.tar.gz` and SHA-256 checksums, and attaches them to GitHub Release assets.

---

## Design System Validation / Validação de Design

UI implementation adheres strictly to [Cobalt Lab v2](docs/design/COBALT_LAB.md) and [AGENTS.md](AGENTS.md):
- **Design Tokens Only**: Colors, typography, spacing, radii, and motion durations are defined exclusively via CSS custom properties.
- **Contrast**: Text exceeds 4.5:1 on the worst pixel behind it over the `CreatureField` background via 88% opacity `.plate` scrims.
- **Motion Budget**: Respects `prefers-reduced-motion` with static renders and zero-transform transitions.
- **Semantics**: `<abbr title="...">` is strictly confined to body prose paragraphs (never in headers, buttons, or metadata labels).
- **Decisions Log**: Architectural choices are documented in [docs/DECISIONS.md](docs/DECISIONS.md) and design adaptations in [docs/design/DECISIONS.md](docs/design/DECISIONS.md).
