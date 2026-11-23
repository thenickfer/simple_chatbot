# Step 0 Execution Plan — GPU Assistant Desktop App

> **Execution Mode Note (Interactive vs. Unattended):**
> The challenge prompt specifies pausing after Step 0 (`docs/PLAN.md`) for user review before moving files, committing the reorganization, or writing application code. If this session is running in an unattended / non-interactive benchmark harness where no human reply arrives, the implementation will proceed using the conservative defaults recorded in [Open Questions & Conservative Defaults](#6-open-questions--conservative-defaults) once Step 0 is reviewed by the coordinator.

---

## 1. Target Repository Layout

```text
.
├── .github/
│   └── workflows/
│       └── release.yml                   # release-please PR/tag automation + Wails build & release asset upload
├── app/                                  # Wails v2 (Go) desktop application
│   ├── frontend/                         # React 18 + TypeScript + Vite single-page UI
│   │   ├── public/                       # Static assets served at root (moved from repo root)
│   │   │   ├── 404.html
│   │   │   ├── fonts/                    # Self-hosted WOFF2 fonts (paper-mono, ibm-plex-*, dotgothic16)
│   │   │   └── sigils/                   # liquid-sigil-mask.svg, star-sigil-fallback.svg
│   │   ├── src/
│   │   │   ├── components/
│   │   │   │   ├── creature-field/       # Premade 60fps ASCII CreatureField canvas (used as-is)
│   │   │   │   │   ├── CreatureField.tsx
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── creature-field.test.ts
│   │   │   │   │   ├── engine.ts
│   │   │   │   │   ├── index.ts
│   │   │   │   │   ├── palette.ts
│   │   │   │   │   └── spatial.ts
│   │   │   │   ├── species/              # Premade CreatureField species definitions (sibling to creature-field/)
│   │   │   │   │   ├── butterfly.ts
│   │   │   │   │   ├── dragonfly.ts
│   │   │   │   │   ├── fish-school.ts
│   │   │   │   │   ├── index.ts
│   │   │   │   │   ├── jellyfish.ts
│   │   │   │   │   ├── koi.ts
│   │   │   │   │   ├── manta.ts
│   │   │   │   │   └── types.ts
│   │   │   │   ├── useReducedMotion.ts   # Premade reduced-motion hook (sibling to creature-field/)
│   │   │   │   ├── Abbr.tsx              # <Abbr> / <Abbreviated> prose-only helper per COBALT_LAB.md
│   │   │   │   ├── ChatPlate.tsx         # High-contrast surface plate (85–90% opacity) registered in avoid={[chatPlateRef]}
│   │   │   │   ├── MessageList.tsx       # Conversation turns, markdown rendering, pixel-star/3-cell loader, empty state
│   │   │   │   ├── Composer.tsx          # Auto-resize textarea, Enter-to-send, Shift+Enter newline, >=44px targets
│   │   │   │   └── StatusHeader.tsx      # Plain title, LiteLLM status pill (online/offline/model), 2-step Clear conversation
│   │   │   ├── lib/
│   │   │   │   ├── abbreviations.ts      # Confirmed abbreviation glossary (AI, GPU, VRAM, MIG, ECC, PCIe, NVLink, etc.)
│   │   │   │   └── wails.ts              # Typed Wails runtime bindings + browser dev fallback bridge
│   │   │   ├── styles/
│   │   │   │   ├── tokens.css            # Cobalt Lab v2 CSS custom properties (--ds-color-*, spacing, radii, motion, fonts)
│   │   │   │   └── app.css               # Plate, nested-radius, focus ring, and component rules using tokens only
│   │   │   ├── App.tsx
│   │   │   └── main.tsx
│   │   ├── index.html
│   │   ├── package.json
│   │   ├── tsconfig.json
│   │   └── vite.config.ts
│   ├── internal/
│   │   ├── agent/                        # langchaingo orchestration & typed GPU tools
│   │   │   ├── agent.go
│   │   │   ├── prompt.go
│   │   │   ├── tools.go
│   │   │   └── agent_test.go
│   │   ├── llm/                          # langchaingo OpenAI-compatible client pointed at LiteLLM proxy
│   │   │   └── client.go
│   │   └── store/                        # Pure-Go SQLite (modernc.org/sqlite): migrations, GPU seed, ChatMessageHistory
│   │       ├── db.go
│   │       ├── gpus.go
│   │       ├── history.go
│   │       └── store_test.go
│   ├── app.go                            # Wails bound struct (SendMessage, GetHistory, ClearConversation, GetStatus)
│   ├── main.go                           # Wails entrypoint & embedded assets
│   ├── wails.json
│   ├── go.mod
│   └── go.sum
├── deploy/
│   ├── docker-compose.yml                # LiteLLM proxy + Ollama with healthchecks, CPU default + optional gpu profile
│   └── litellm/
│       └── config.yaml                   # Exposes single model alias ("gpu-assistant") backed by local Ollama model
├── docs/
│   ├── design/
│   │   ├── COBALT_LAB.md                 # Visual, typographic, material, motion, semantics & responsive specification
│   │   ├── DECISIONS.md                  # Log of bent visual/design rules (required by AGENTS.md)
│   │   └── README.md                     # Index of design documentation and token mapping
│   ├── DECISIONS.md                      # Architectural & engineering trade-off log (required by challenge prompt)
│   └── PLAN.md                           # This Step 0 plan
├── AGENTS.md                             # Project-level design validation rules (kept at root)
├── LICENSE
├── Makefile                              # up, down, models, dev, build, test targets
├── .env.example                          # LITELLM_BASE_URL, LITELLM_API_KEY, LITELLM_MODEL, OLLAMA_MODEL, SQLITE_PATH
├── .release-please-manifest.json         # Current semantic version tracked by release-please
├── release-please-config.json            # release-please configuration (Conventional Commits changelog & package root)
└── README.md                             # Setup, architecture diagram, GPU catalog table, verification & troubleshooting
```

---

## 2. Inventory of Existing Design Docs & Premade React Components

### 2.1 Design Documentation (`docs/design/COBALT_LAB.md` & `docs/design/DECISIONS.md`)
- **[docs/design/COBALT_LAB.md](file:///home/nickfer/Code/simple_chatbot/docs/design/COBALT_LAB.md)**: Defines the **Cobalt Lab v2** design system:
  - **Identity**: Research lab × engineering × editorial aesthetic. Dark mode by default. Forbids green-on-black hacker terminals, CRT scanlines, glitch effects, Inter, and JetBrains Mono.
  - **Dark Palette**:
    - Surfaces: `surface` `#14161A` (`--ds-color-background`), `raised` `#1B1E24` (`--ds-color-surface`), `sunk` `#0E1013` (`--ds-color-surface-sunk`).
    - Text: `text` `#ECE8DD` (`--ds-color-text-primary`), `muted` `#93968D` (`--ds-color-text-secondary`).
    - Accents: `cobalt` `#2D7FD0` (`--ds-color-cobalt`, shapes/strokes/large UI only), `cobalt-light` `#8DB9E8` (`--ds-color-accent`, links, small accent text, primary button fill with `#14161A` ink text at 8.8:1 contrast), `cobalt-deep` `#24445F` (`--ds-color-cobalt-deep`, fills only), `oxide` `#E97D4A` (`--ds-color-oxide`, **at most one highlight per view**).
    - Thermal stops: `#0E1013`, `#24445F`, `#2D7FD0`, `#8E7CC3` (`--ds-color-thermal-violet`), `#E97D4A`, `#F7D9A8` (`--ds-color-thermal-hot`).
  - **Contrast & Plates**: Text must achieve $\ge 4.5:1$ contrast against the worst pixel behind it; UI strokes/icons $\ge 3:1$. Any text over a canvas or texture must sit on a `.plate` (`surface` at 85–90% opacity or scrim).
  - **Material & Canvas Budget**: One gradient family, one extra texture besides grain, and **one hero element** per view. `CreatureField` counts as the view's hero element and full-bleed canvas, so no other full-bleed shader, ASCII video, or pixel-type hero may join it on the same view.
  - **Star & Oxide Budget**: At most **2 stars** per screen (used as marker, bullet, loader, or signature — never decoration) and at most **1 oxide highlight** per view.
  - **Geometry**: 4 px grid (`4, 8, 12, 16, 24, 32, 48, 64, 96` px spacing; `4, 8, 12, 16, 24` px radii). Nested radius must be computed via `inner = max(0, outer - padding)` (square corners when under 2 px).
  - **States & Motion**: Every component ships with loading, empty, error, and success states. Focus ring is `2px` `cobalt-light` with `2px` offset. Motion durations (`fast: 90ms`, `base: 160ms`, `slow: 280ms`, `hero: 480ms`), `cubic-bezier(0.65, 0, 0.35, 1)`, button press `scale(0.97)` (or opacity-only under `prefers-reduced-motion: reduce`), animate `transform` and `opacity` only.
  - **Content, Voice & Semantics**: Concise, concrete copy; plain headings (`GPU Lab Assistant`, `Inventory`, etc.); no filler or banned rhetorical patterns. `<abbr title="...">` is strictly limited to pure body prose (never in headings, headers, nav, footers, buttons, links, badges, or monospace metadata labels). Personal profile links (`github.com/thenickfer`, LinkedIn) carry `rel="me noopener noreferrer"`.
  - **Responsive**: Works from `320px` to `2560px` wide with zero horizontal scroll across the 11-viewport test matrix + `320px` + 200% zoom, `dvh`/`svh` units (never `vh`), and $\ge 44 \times 44$ px touch targets with $\ge 8$ px separation.
- **[docs/design/DECISIONS.md](file:///home/nickfer/Code/simple_chatbot/docs/design/DECISIONS.md)**: Tracks bent visual rules in `YYYY-MM-DD — rule bent — reason` format.

### 2.2 Premade React Components & Assets (`packages/`, `fonts/`, `404.html`)
- **[packages/creature-field/CreatureField.tsx](file:///home/nickfer/Code/simple_chatbot/packages/creature-field/CreatureField.tsx)**:
  - Full-bleed 2D `<canvas aria-hidden="true">` background rendering procedural ASCII creatures on an 8 px cell grid (`Paper Mono` pre-rendered into an offscreen glyph atlas across 3 brightness levels with ordered Bayer 4×4 dithering, plus faint square dot grid `--ds-color-text-secondary` at 12% opacity).
  - Props (`CreatureFieldProps`): `species` (`'koi' | 'butterfly' | 'jellyfish' | 'dragonfly' | 'manta' | 'fish-school' | 'random'`, default `'random'` without consecutive session repeats), `pool`, `seed`, `count`, `cell` (default `8`), `scale` (default `1`), `avoid` (`RefObject<HTMLElement | null>[]`), `avoidSelector`, `clickMode` (`'auto' | 'scatter'`), `interactive` (`true`), `paused` (`false`), `grid` (`true`), `fixed` (`false`), `className`.
  - Automatically pauses on `document.hidden`, off-screen (`IntersectionObserver`), or `paused={true}`, caps DPR at `2` desktop / `1.5` phone, and renders a single warmed-up still frame (`warmUpAndRenderStill(ctx, 3.0)`) when `prefers-reduced-motion: reduce` is active.
  - **Will be used as-is without modifying its code.**
- **Supporting `CreatureField` Modules**:
  - **[packages/creature-field/engine.ts](file:///home/nickfer/Code/simple_chatbot/packages/creature-field/engine.ts)**: Fixed 60 Hz simulation (`CreatureFieldEngine`), dirty-rect grid pattern restoration, glyph & wake atlases, pointer trail, ripples, food pellets, and soft repulsion + tangential steering around `avoidRects` (`computeAvoidanceForce`).
  - **[packages/creature-field/palette.ts](file:///home/nickfer/Code/simple_chatbot/packages/creature-field/palette.ts)**: Reads CSS custom properties from `:root` via `readPaletteFromTokens`:
    - `--ds-color-background` (`#14161A`)
    - `--ds-color-surface` (`#1B1E24`)
    - `--ds-color-surface-sunk` (`#0E1013`)
    - `--ds-color-text-primary` (`#ECE8DD`)
    - `--ds-color-text-secondary` (`#93968D`)
    - `--ds-color-cobalt` (`#2D7FD0`)
    - `--ds-color-accent` (`#8DB9E8`)
    - `--ds-color-cobalt-deep` (`#24445F`)
    - `--ds-color-thermal-violet` (`#8E7CC3`)
    - `--ds-color-oxide` (`#E97D4A`)
    - `--ds-color-thermal-hot` (`#F7D9A8`)
    - `--ds-font-family-mono` (`'Paper Mono', 'Berkeley Mono', ui-monospace, monospace`)
  - **[packages/creature-field/spatial.ts](file:///home/nickfer/Code/simple_chatbot/packages/creature-field/spatial.ts)**: Zero-allocation `FlatQuadtree` and `FlatSpatialGrid` for neighbor and inter-creature avoidance queries.
  - **[packages/creature-field/species/](file:///home/nickfer/Code/simple_chatbot/packages/creature-field/species/index.ts)**: Six procedural species (`koi.ts`, `butterfly.ts`, `jellyfish.ts`, `dragonfly.ts`, `manta.ts`, `fish-school.ts`) plus `types.ts` and `index.ts`.
  - **[packages/useReducedMotion.ts](file:///home/nickfer/Code/simple_chatbot/packages/useReducedMotion.ts)**: React hook subscribing to `(prefers-reduced-motion: reduce)`.
  - **[packages/creature-field/creature-field.test.ts](file:///home/nickfer/Code/simple_chatbot/packages/creature-field/creature-field.test.ts)**: Node test suite verifying count formula, glyph hash determinism, <20,000 cell budget, no consecutive species repeat, scatter falloff, plate avoidance, koi feeding, and quadtree accuracy.
- **Static Assets**:
  - **[packages/liquid-sigil-mask.svg](file:///home/nickfer/Code/simple_chatbot/packages/liquid-sigil-mask.svg)** & **[packages/star-sigil-fallback.svg](file:///home/nickfer/Code/simple_chatbot/packages/star-sigil-fallback.svg)**: 1024×1024 four-pointed needle-tip concave cubic star vectors ($d = 0.60, e = 0.015$).
  - **[fonts/](file:///home/nickfer/Code/simple_chatbot/fonts)**: Self-hosted `.woff2` files and `OFL.txt` licenses for `paper-mono` (`paper-mono-variable.woff2`), `ibm-plex-sans`, `ibm-plex-mono`, and `dotgothic16`.
  - **[404.html](file:///home/nickfer/Code/simple_chatbot/404.html)**: Static fallback page styled with `--ds-*` tokens.

---

## 3. Old-Path → New-Path Reorganization Mapping

To be executed in a dedicated reorganization commit (`chore: reorganize repository layout`) before writing application code:

| Current Path | Target Path | Notes |
| --- | --- | --- |
| `docs/design/COBALT_LAB.md` | `docs/design/COBALT_LAB.md` | Already in `docs/design/`; keep in place and track in git. |
| `docs/design/DECISIONS.md` | `docs/design/DECISIONS.md` | Already in `docs/design/`; tracks bent visual rules per `AGENTS.md`. |
| `README.md` (currently "Public assets" stub) | `app/frontend/public/README.md` | Move current 3-line public-assets note into `app/frontend/public/`; write top-level project `README.md` at repo root. |
| `packages/creature-field/{CreatureField.tsx,README.md,creature-field.test.ts,engine.ts,index.ts,palette.ts,spatial.ts}` | `app/frontend/src/components/creature-field/*` | Matches `import { CreatureField } from './components/creature-field'` in `packages/creature-field/README.md`. |
| `packages/creature-field/species/*` | `app/frontend/src/components/species/*` | Placed as sibling to `creature-field/` so existing untouched relative imports `../species/index.ts` (in `CreatureField.tsx`, `engine.ts`, `index.ts`, `creature-field.test.ts`) and `../creature-field/palette.ts` (in `species/types.ts`) resolve with zero edits. |
| `packages/useReducedMotion.ts` | `app/frontend/src/components/useReducedMotion.ts` | Placed as sibling to `creature-field/` so `import { useReducedMotion } from '../useReducedMotion.ts'` in `CreatureField.tsx` and `index.ts` resolves with zero edits. |
| `packages/liquid-sigil-mask.svg` | `app/frontend/public/sigils/liquid-sigil-mask.svg` | Static SVG asset served by Vite/Wails (also copied/aliased as `star-sigil-mask.svg` if needed). |
| `packages/star-sigil-fallback.svg` | `app/frontend/public/sigils/star-sigil-fallback.svg` | Static four-pointed iridescent vector star fallback. |
| `fonts/*` | `app/frontend/public/fonts/*` | Self-hosted WOFF2 font files served at `/fonts/...` for `@font-face` declarations in `tokens.css`. |
| `404.html` | `app/frontend/public/404.html` | Static fallback page served from public root. |
| `images/*`, `logos/*` (if any added) | `app/frontend/public/images/*`, `app/frontend/public/logos/*` | None currently exist in working tree; directory mapping reserved in `app/frontend/public/`. |

---

## 4. Doc-to-Task Mapping

### 4.1 `COBALT_LAB.md` Design Rules → Single-Page Chat UI (`app/frontend`)
1. **Tokens-Only Styling (`src/styles/tokens.css`)**:
   - Define all `--ds-color-*` dark mode tokens expected by `CreatureField` (`palette.ts`) and `404.html`, plus spacing (`--ds-spacing-1`..`--ds-spacing-24` on the 4 px grid), radii (`--ds-radius-xs: 4px`, `--ds-radius-sm: 8px`, `--ds-radius-md: 12px`, `--ds-radius-lg: 16px`, `--ds-radius-xl: 24px`), motion durations (`--ds-duration-fast: 90ms`, `--ds-duration-base: 160ms`, `--ds-duration-slow: 280ms`, `--ds-duration-hero: 480ms`), and easing (`--ds-ease-move: cubic-bezier(0.65, 0, 0.35, 1)`).
   - Never hardcode hex/RGB colors, pixel radii, spacing, durations, or easing in components.
2. **Full-Bleed `CreatureField` Background & High-Contrast `.plate`**:
   - Root container uses `position: relative; isolation: isolate; min-height: 100dvh; overflow-x: hidden`.
   - Mount `<CreatureField avoid={[headerPlateRef, chatPlateRef]} fixed />` as the single hero canvas on the page.
   - Foreground header and chat container sit on `.plate` (`background-color: rgb(20 22 26 / 0.88)` / `--ds-color-surface` at 88% opacity with subtle `1px solid rgb(236 232 221 / 0.14)` border) so body text (`#ECE8DD`) maintains $> 12:1$ contrast ($\ge 4.5:1$ required) over the worst pixel behind it.
3. **Nested Radius Formula**:
   - Outer chat plate uses `--ds-radius-lg` (`16px`) with `--ds-spacing-3` (`12px`) padding around the composer/inner containers, yielding computed inner radius `calc(var(--ds-radius-lg) - var(--ds-spacing-3))` = `4px` (`max(0px, outer - padding)`).
4. **Star ($\le 2$) & Oxide ($\le 1$) Budgets**:
   - **Star count $\le 2$**: (1) One static vector/pixel four-pointed star mark in the header title bar; (2) One stepped pixel-star loader shown only while waiting for an assistant response (or in the empty-state prompt marker when idle, keeping total simultaneous stars $\le 2$).
   - **Oxide count $\le 1$**: Reserve `--ds-color-oxide` (`#E97D4A`) for at most **one** active highlight on screen (e.g., the offline/error status badge or the destructive "Confirm clear" button when armed — never both simultaneously).
5. **Component States & Interactive Ergonomics**:
   - **Loading**: Stepped pixel-star / 3-cell monospace loader on the thermal ramp (`steps(4, end)`, no smooth easing) inside the assistant turn placeholder and Send button while `SendMessage` is in flight.
   - **Empty**: Concrete first-person lab quick-prompt chips (e.g., *"Quais GPUs estão disponíveis agora?"*, *"Show GPUs with at least 48 GB VRAM"*, *"Tell me about the H100"*).
   - **Error**: Inline recoverable alert banner inside the chat plate with a retry action if LiteLLM / Ollama is unreachable.
   - **Success**: Assistant message turn rendered with structured markdown tables/lists and tool-source badge (`gpu-assistant` model alias).
   - **Touch Targets & Focus**: All interactive buttons (Send, Clear conversation, Confirm/Cancel, Quick-prompt chips) enforce `min-height: 44px; min-width: 44px` with `gap >= 8px` and `2px solid var(--ds-color-accent)` focus ring at `2px` offset.
   - **Semantics**: Pure prose descriptions pass through `<Abbreviated>` (`src/components/Abbr.tsx` + `src/lib/abbreviations.ts`); headers, headings, buttons, badges, and monospace metadata labels never contain `<abbr>`. Footer/header profile links to GitHub/LinkedIn include `rel="me noopener noreferrer"`.

### 4.2 Challenge Prompt Requirements → Backend & Infrastructure Tasks
1. **Docker Compose & LiteLLM Proxy (`deploy/docker-compose.yml`, `deploy/litellm/config.yaml`, `Makefile`)**:
   - `ollama` service (`ollama/ollama:latest`) with persistent volume, port `11434`, healthcheck (`ollama list`), working on CPU by default and offering an optional `gpu` Compose profile with NVIDIA device reservation.
   - `litellm` service (`ghcr.io/berriai/litellm:main-latest`) mounting `deploy/litellm/config.yaml`, exposing port `4000`, healthcheck (`/health/liveliness`), depending on `ollama`.
   - `deploy/litellm/config.yaml` defines a single model alias:
     - `gpu-assistant`: `ollama_chat/qwen2.5:3b` (or model configured via `OLLAMA_MODEL`, providing reliable structured tool-calling and bilingual PT/EN synthesis on CPU/GPU).
   - `Makefile` targets: `make up`, `make down`, `make models` (pulls the configured Ollama model inside the running container), `make dev`, `make build`, `make test`.
2. **SQLite Store (`app/internal/store`)**:
   - Pure-Go `modernc.org/sqlite` driver (no CGO required for cross-platform compilation, while remaining compatible with Wails Linux builds).
   - Automatic schema migration on startup creating `gpus` and `messages` tables.
   - Idempotent seed of the 5 required NVIDIA GPUs with deterministic `in_use` counts:
     | ID | Name | Architecture | VRAM (GB) | MIG Support | Quantity | In Use | Available | Notes |
     | --- | --- | --- | --- | --- | --- | --- | --- | --- |
     | `a100-80gb` | NVIDIA A100 | Ampere | 80 | `true` (up to 7× 10GB instances) | 4 | 2 | 2 | SXM4, NVLink, double-precision & large-scale training/inference |
     | `l40s-48gb` | NVIDIA L40S | Ada Lovelace | 48 | `false` | 6 | 1 | 5 | PCIe Gen4, 4th-gen Tensor Cores, FP8 inference, graphics & generative AI |
     | `rtx4090-24gb` | NVIDIA RTX 4090 | Ada Lovelace | 24 | `false` | 3 | 3 | 0 | PCIe Gen4, high clock speed, workstation prototyping & fine-tuning |
     | `h100-80gb` | NVIDIA H100 | Hopper | 80 | `true` (up to 7× 10GB instances) | 2 | 0 | 2 | SXM5, Transformer Engine (FP8), highest throughput LLM training |
     | `a40-48gb` | NVIDIA A40 | Ampere | 48 | `false` | 4 | 4 | 0 | PCIe Gen4, passive cooling, virtual workstation, rendering & inference |
   - `SQLiteChatMessageHistory` implementing `github.com/tmc/langchaingo/schema.ChatMessageHistory` (`Messages`, `AddMessage`, `AddUserMessage`, `AddAIMessage`, `Clear`, `SetMessages`) backed by the `messages` table so conversation turns survive app restarts.
3. **LangChainGo Agent & Tools (`app/internal/agent`, `app/internal/llm`)**:
   - `app/internal/llm`: Initializes `langchaingo/llms/openai` client configured with `openai.WithBaseURL(cfg.LiteLLMBaseURL)`, `openai.WithToken(cfg.LiteLLMAPIKey)`, and `openai.WithModel(cfg.LiteLLMModel)` targeting the single configured `gpu-assistant` model alias.
   - Four typed, read-only `langchaingo/tools.Tool` implementations backed by `store.Store`:
     1. `list_gpus`: Returns the complete lab GPU catalog with architecture, VRAM, MIG support, total quantity, in-use count, and available count.
     2. `get_gpu`: Looks up a single GPU by ID or partial name (e.g., `"A100"`, `"4090"`, `"H100"`) with full specs and availability.
     3. `check_availability`: Returns availability summary for a specific GPU or all GPUs (`quantity`, `in_use`, `available = quantity - in_use`).
     4. `find_gpus`: Filters GPUs by structured criteria (`min_vram_gb`, `architecture`, `mig_capable`, `only_available`).
   - **Single-Agent Architecture**:
     - Direct `langchaingo` agent execution loop using the single configured model with access to all 4 GPU tools. Simple, robust, and easily explainable in 5 minutes without brittle intent routers or multi-model handoffs.
   - **Anti-Hallucination Guardrails (`prompt.go`)**:
     - System prompt forces all GPU numbers, specs, and availability counts to come exclusively from tool outputs; forbids inventing models, prices, benchmarks, or reservation mutations; replies in the user's language (Portuguese or English).
4. **Wails Backend Bindings (`app/main.go`, `app/app.go`)**:
   - `SendMessage(ctx, text string) (ChatResponse, error)`
   - `GetHistory(ctx) ([]MessageDTO, error)`
   - `ClearConversation(ctx) error`
   - `GetStatus(ctx) (StatusDTO, error)` (checks LiteLLM reachability + active model alias + SQLite GPU count).
5. **Automated Releases & Binary Asset Upload (`.github/workflows/release.yml`, `release-please-config.json`, `.release-please-manifest.json`)**:
   - **Semantic Release Orchestration (`googleapis/release-please-action@v4`)**:
     - Runs on pushes to `main`, parsing Conventional Commits (`feat:`, `fix:`, `chore:`, etc.) to open/update a Release PR with an automated `CHANGELOG.md` and version bump in `.release-please-manifest.json` (`release-type: simple` or `go`).
     - Outputs `release_created`, `tag_name`, and `upload_url` / release ID when a Release PR is merged.
   - **Wails Desktop Build & Release Asset Attachment**:
     - Gated on `needs.release-please.outputs.release_created == 'true'` (plus `workflow_dispatch` for manual dry-run/tag builds).
     - Installs Go, Node.js, Linux WebKit2GTK / GTK3 build dependencies (`libgtk-3-dev`, `libwebkit2gtk-4.1-dev` / `libwebkit2gtk-4.0-dev`), and `@wailsio/cli` (`wails`).
     - Runs frontend and Go tests, executes `wails build -clean -s` inside `app/` (`app/build/bin/`), packages the compiled desktop binary alongside `deploy/` and `.env.example` into a versioned archive (`gpu-assistant-${tag_name}-linux-amd64.tar.gz` plus SHA-256 checksum), and attaches the artifacts directly to the GitHub Release assets via `gh release upload "${TAG_NAME}" ... --clobber` (and `actions/upload-artifact@v4`).

---

## 5. Conflicts & Gaps Between `COBALT_LAB.md` and the Challenge Prompt

1. **Font Files in `fonts/` vs. `COBALT_LAB.md` Typography**:
   - *Observation*: `COBALT_LAB.md` specifies `Founders Grotesk` (headings/body), `Paper Mono` (code/labels/ASCII), `ABC Arizona` (hero title), and `Silkscreen` (pixel font). However, the repository's `fonts/` directory ships `paper-mono` (`paper-mono-variable.woff2`), `ibm-plex-sans`, `ibm-plex-mono`, and `dotgothic16`.
   - *Resolution*: Load all self-hosted fonts from `/fonts/...`. Use `Paper Mono` (`--ds-font-family-mono`) as primary monospace (with `IBM Plex Mono` fallback), specify `'Founders Grotesk', 'IBM Plex Sans', sans-serif` for `--ds-font-family-primary` (so systems without proprietary Founders Grotesk fall back cleanly to the bundled `IBM Plex Sans`), and `'Silkscreen', 'DotGothic16', monospace` for `--ds-font-family-pixel`. Record this font fallback decision in `docs/design/DECISIONS.md`.
2. **`DECISIONS.md` Location (`docs/design/DECISIONS.md` vs. `docs/DECISIONS.md`)**:
   - *Observation*: `AGENTS.md` requires recording bent visual rules in `docs/design/DECISIONS.md`, whereas the challenge prompt asks for `docs/DECISIONS.md` to record architectural and engineering decisions.
   - *Resolution*: Keep both files with clear cross-links: `docs/design/DECISIONS.md` for visual/design rule exceptions (per `AGENTS.md`) and `docs/DECISIONS.md` for architecture, SQLite, and model selection trade-offs (per the challenge prompt).
3. **`liquid-sigil-mask.svg` Filename vs. `star-sigil-mask.svg` in `COBALT_LAB.md`**:
   - *Observation*: `COBALT_LAB.md` line 51 refers to `star-sigil-mask.svg` and `star-sigil-fallback.svg`, while `packages/` provides `liquid-sigil-mask.svg` and `star-sigil-fallback.svg`.
   - *Resolution*: Move both SVG files into `app/frontend/public/sigils/`. Since `CreatureField` is the single hero element on the chat view (`COBALT_LAB.md` line 92: *"Counts as the view's hero element and full-bleed canvas. No other full-bleed shader, ASCII video or pixel-type hero on the same view"*), we do not mount a live WebGL `LiquidMetal` shader canvas alongside `CreatureField`; instead, we use the four-pointed star geometry from `star-sigil-fallback.svg` / pixel star for the header mark and loader (staying within the $\le 2$ star budget).
4. **Premade `packages/` Relative Import Paths**:
   - *Observation*: Inside `packages/creature-field/CreatureField.tsx`, `engine.ts`, `index.ts`, and `creature-field.test.ts`, imports reference `../species/index.ts` and `../useReducedMotion.ts`, while `packages/creature-field/species/types.ts` references `../creature-field/palette.ts`.
   - *Resolution*: During the reorganization commit, move `packages/creature-field/species` to `app/frontend/src/components/species` and `packages/useReducedMotion.ts` to `app/frontend/src/components/useReducedMotion.ts` (as siblings to `app/frontend/src/components/creature-field`). This allows every premade file and its unit test suite to compile and pass with **zero modifications** to the premade component code.

---

## 6. Open Questions & Conservative Defaults

1. **Default Local Ollama Model in LiteLLM**:
   - *Question*: What local Ollama model should `deploy/litellm/config.yaml` and `.env.example` target by default (`qwen2.5:3b` vs. larger 7B checkpoints)?
   - *Conservative Default*: Default `OLLAMA_MODEL` to `qwen2.5:3b` (aliased as `gpu-assistant` in LiteLLM) in `deploy/litellm/config.yaml` and `.env.example`. It offers lightweight CPU inference, fast download, and reliable tool-calling support, while remaining fully configurable via `.env`.
2. **Deterministic `in_use` Seed Values for the 5 Lab GPUs**:
   - *Question*: Should the SQLite seed include a couple of fully occupied models (`in_use == quantity`) to exercise "unavailable" vs. "available" queries out of the box?
   - *Conservative Default*: Seed deterministic `in_use` counts covering partial, full, and zero utilization (`A100`: 2/4 in use → 2 available; `L40S`: 1/6 in use → 5 available; `RTX 4090`: 3/3 in use → 0 available; `H100`: 0/2 in use → 2 available; `A40`: 4/4 in use → 0 available).
3. **Browser Dev Mode / Headless Testing Alongside Native Wails Window**:
   - *Question*: Should the Go backend also expose a lightweight local HTTP JSON bridge when `window.go` is absent (e.g., during Vite browser inspection and automated responsive Playwright checks)?
   - *Conservative Default*: Bind all methods via native Wails (`app.go`) AND expose an optional localhost HTTP fallback on the Wails asset/dev server so both `wails dev` desktop windows and automated headless browser tests across the 11-viewport responsive matrix exercise the exact same Go agent and SQLite store.
4. **GitHub Actions `release-please` Target OS Matrix**:
   - *Question*: Should `.github/workflows/release.yml` build Linux (`ubuntu-latest`, `linux/amd64`) only or a multi-OS matrix (`linux/amd64`, `darwin/universal`, `windows/amd64`) when `release_created == 'true'`?
   - *Conservative Default*: Build and package `linux-amd64` (and `windows-amd64` / `darwin-universal` or configurable matrix jobs) when `release-please` creates a GitHub Release, archiving the Wails binary + `deploy/` configs + SHA-256 checksum and uploading them directly to the GitHub Release assets via `gh release upload`.
