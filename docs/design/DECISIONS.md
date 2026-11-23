# Design decisions

Format: `YYYY-MM-DD — rule bent — reason`

- `2026-11-13 — LiquidSigil & GrainGradient shader tuning — Tuned LiquidSigil named constant (scale 0.97 to match the 70% pre-padded SVG mask and eliminate the oversized 40% fallback border overhang, shiftRed/shiftBlue 0.5 and repetition 3 / softness 0.06 / contour 0.18 for a thinner, more iridescent border, 512px PNG mask rasterization to avoid 4096px SVG CPU stall) and GrainGradient (speed 0.78 to compensate for shader 0.1x time multiplier, maxPixelCount capped at 640k desktop / 320k phone to eliminate fragment lag).`
- `2026-11-23 — Typography font fallbacks — Bundled open self-hosted WOFF2 fonts (Paper Mono, IBM Plex Sans, IBM Plex Mono, DotGothic16) in app/frontend/public/fonts/ to ensure offline, self-contained desktop rendering without external CDNs. Specified 'Founders Grotesk', 'IBM Plex Sans', sans-serif for primary typography, 'Paper Mono', 'Berkeley Mono', 'IBM Plex Mono', monospace for monospace/ASCII typography, and 'Silkscreen', 'Press Start 2P', 'DotGothic16', monospace for pixel typography.`
