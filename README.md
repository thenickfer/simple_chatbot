# GPU Lab Assistant

> Desktop assistant for querying GPU inventory in real time.  
> Built with **Wails v3 (Go + GTK4 / WebKitGTK 6.0)**, **React + TypeScript**, **LangChainGo**, **LiteLLM**, **Ollama**, and my own design specs.

---

## Architecture Overview / Visão Geral da Arquitetura

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Wails Desktop Application                       │
│                                                                        │
│   ┌────────────────────────────────┐   ┌───────────────────────────┐   │
│   │   Frontend (React + Vite)      │   │     Backend (Go)          │   │
│   │   • Cobalt Lab v2 Dark Design  │IPC│   • Wails v3 Native GTK4  │   │
│   │   • 60fps CreatureField Canvas │───│   • LangChainGo Agent     │   │
│   │   • High-Contrast UI Plates    │   │   • 5 Typed SQLite Tools  │   │
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

The architecture is deliberately modular (see [docs/DECISIONS.md](docs/DECISIONS.md) for architectural rationale and trade-offs):
1. **Frontend**: React single page with an AI chat and clear conversation control.
2. **Backend**: Pure-Go agent using `langchaingo` with 5 typed SQLite tools (4 read-only inventory tools + `borrow_or_return_gpu` guarded by transactional stock/ownership checks and `GPU_LOAN_PASSWORD` verified inside the store), plus timestamped trace logging (`app/logs/agent-*.log`). Runs natively on desktop via Wails v3 (GTK4 + WebKitGTK 6.0) or standalone HTTP mode.
3. **LLM Proxy**: Dockerized LiteLLM proxy exposing a standard OpenAI-compatible API to the Go app, routing queries to a local Ollama instance.

---

## Simulated GPU Inventory 

The local SQLite database initializes idempotently with 5 enterprise NVIDIA GPUs and deterministic allocation states:

| GPU Model | Architecture | VRAM | MIG Capable | Total Quantity | In Use | Available | Primary Workloads |
| :--- | :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| **NVIDIA A100** | Ampere | 80 GB | Yes (up to 7x) | 4 | 2 | **2** | Large-scale LLM training, FP64 scientific simulations |
| **NVIDIA L40S** | Ada Lovelace | 48 GB | No | 6 | 1 | **5** | Generative AI, FP8 inference, omniverse graphics |
| **NVIDIA RTX 4090** | Ada Lovelace | 24 GB | No | 3 | 3 | **0** | Workstation prototyping, fine-tuning smaller models |
| **NVIDIA H100** | Hopper | 80 GB | Yes (up to 7x) | 2 | 0 | **2** | State-of-the-art Transformer engine training/inference |
| **NVIDIA A40** | Ampere | 48 GB | No | 4 | 4 | **0** | Virtual workstations, graphics rendering, visual computing |

I took the liberty to add some more info on the GPUs so the assistant can be more helpful when choosing a model.
---

## Prerequisites 

- **Docker & Docker Compose** (for Ollama and LiteLLM)
- **Go 1.26+**
- **Node.js 24+ (or 22.18+) & npm** *(required for native `.ts` execution in `node --test`)*
- Linux desktop build dependencies:
  - Arch Linux: `sudo pacman -S gtk4 webkitgtk-6.0` (or `gtk3 webkit2gtk-4.1` on older distros)
  - Ubuntu / Debian: `sudo apt-get install -y libgtk-4-dev libwebkitgtk-6.0-dev` (or `libgtk-3-dev libwebkit2gtk-4.1-dev`)

---

## Quick Start

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

---

## Adding or Switching Models

The architecture decouples the desktop app from the LLM provider via LiteLLM. You can switch models or providers **purely by editing `.env`** without changing any code or recompiling the Go binary.

**I** personally use a local llama.cpp instance which I was given access to.

### Option A: Google Gemini Free Tier API (`gemini-2.5-flash`) — Recommended Cloud Option

Google AI Studio provides a free tier for Gemini models with generous rate limits. To use Gemini:

1. Obtain a free API key at [Google AI Studio](https://aistudio.google.com/app/apikey).
2. In your `.env` file, set:
   ```env
   GEMINI_API_KEY=AIzaSy...your_gemini_api_key_here...
   LITELLM_MODEL=gemini
   ```
3. Restart LiteLLM:
   ```bash
   make down && make up
   ```
> **Automatic Fallback Guarantee**: If Gemini encounters network issues or rate limits, LiteLLM's configured router automatically falls back to your local Ollama instance (`gpu-assistant-local`) so the chat app never fails.

### Option B: Alternative Local Ollama Models (100% Offline & Private)

To change the local model:
1. In `.env`, change `OLLAMA_MODEL`:
   ```env
   OLLAMA_MODEL=llama3.2:3b
   # or: OLLAMA_MODEL=mistral:7b
   # or: OLLAMA_MODEL=deepseek-r1:1.5b
   ```
2. Pull the new model and restart:
   ```bash
   make models
   make down && make up
   ```

---

### 4. Run Smoke Test
```bash
make smoke
```
*Verifies that LiteLLM is responsive and the `gpu-assistant` model alias is registered.*

### 5. Launch Application
```bash
# Run application in development mode (launches native Wails v3 window when display is present, or standalone server):
make dev

# Or launch standalone browser server directly:
make dev-server

# Production build:
make build
```

---

## Usage Examples & Trace Logging

Example questions you can ask the assistant (Portuguese or English):

- **"Quais GPUs existem na infraestrutura?"** → Calls `list_gpus` and lists all 5 seeded NVIDIA models (19 total units) with VRAM and simulated availability.
- **"Qual GPU tem mais memória?"** → Reports the 80 GB per-GPU tie (**NVIDIA A100** and **NVIDIA H100**) as well as the largest aggregate available VRAM pool (**NVIDIA L40S**, 240 GB).
- **"Existe alguma NVIDIA B200?"** → Calls `get_gpu`, confirms B200 is not in the catalog, and lists the 5 models that exist.
- **"Quero pegar 1 A100 emprestada (senha: gpu-lab-secret)"** / **"Quero devolver 1 A100 (senha: gpu-lab-secret)"** → Calls `borrow_or_return_gpu`; the backend store validates `GPU_LOAN_PASSWORD`, stock limits, and user loan ownership inside a SQLite transaction.

### Live Debugging / Demo Logs

Every session writes timestamped events (`[USER]`, `[THOUGHT]`, `[TOOL_CALL]` with redacted passwords, `[TOOL_RESULT]`, `[ASSISTANT]`) to `app/logs/agent-YYYYMMDD-HHMMSS.log`:

```bash
tail -f app/logs/agent-*.log
```

See [docs/DECISIONS.md](docs/DECISIONS.md) for architecture decisions, limitations, and future work.