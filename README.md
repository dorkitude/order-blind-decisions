---
title: Order-blind decisions
type: repository-index
status: harness in development; no results yet
created: 2026-10-09
author: Kyle Wild
tags: [primacy, recency, position-bias, decision-models, llm-as-judge]
---

# Order-blind decisions

**Do decision models judge responses differently depending on where those responses appear?**

Language models are known to show **primacy** (favoring what comes first) and **recency** (favoring what comes last) effects, including as judges; see [`sources/`](sources/README.md). This study asks whether *decision models* do too. These are models that return typed answers with probabilities instead of generated text.

| Model | Role |
|---|---|
| Jev (`jev-1.13.0`) | reference; hypothesized to be order-blind |
| OpenAI Decisions (`gpt-6-luna`) | Jev-compatible decision model |
| Cloudflare Clef-flash (`clef-flash`) | Jev-compatible decision model |

Every result is reported both per model and as a paired difference from Jev on identical requests.

```mermaid
flowchart LR
    RB[RewardBench 2<br/>1,865 prompts × 4 responses] --> H[Haystack ordering arm<br/>responses move]
    RB --> Q[Question ordering arm<br/>questions move]
    H --> F1[choice · packed rubric]
    Q --> F1
    RB --> S[solo rubric<br/>no position]
    F1 --> M{order-blind?}
    S --> M
    M -->|90% CI inside ±3 pp / ±0.25| Y[order-blind]
    M -->|CI outside| N[position-biased]
    M -->|straddles| I[inconclusive]
```

## Background: the serial-position curve

In 1962 Bennet Murdock read people lists of words and asked them to recall as many as they could, in any order ([Murdock 1962](https://doi.org/10.1037/h0045106)). Recall by list position traced a **U-shaped curve**:

- **Primacy:** the first few words were remembered better than the middle.
- **Recency:** the last few were remembered best of all.
- **The middle** was a flat trough.

```mermaid
%%{init: {"themeVariables": {"xyChart": {"plotColorPalette": "#2f6fdf"}}}}%%
xychart-beta
    title "Serial-position curve (schematic, shape after Murdock 1962)"
    x-axis "Position in list" [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20]
    y-axis "Chance of recall" 0 --> 1
    line [0.45, 0.35, 0.28, 0.24, 0.21, 0.20, 0.20, 0.19, 0.19, 0.19, 0.19, 0.19, 0.20, 0.20, 0.21, 0.24, 0.31, 0.45, 0.65, 0.85]
```

*Schematic only: the values illustrate the curve's shape and are not Murdock's measurements.*

Language models show the same U. *Lost in the Middle* (Liu et al. 2023) found that models use evidence at the start or end of a long context better than evidence in the middle. LLM judges likewise favor responses by slot rather than by quality. [`sources/`](sources/README.md) collects that literature.

This study asks whether decision models trace the U or a flat line. With four responses per request, slot 1 is the primacy end and slot 4 the recency end:

| Curve | What it would mean here |
|---|---|
| Flat | Order-blind: a response is judged the same in any slot |
| Raised at slot 1 | Primacy: the first response is favored |
| Raised at slot 4 | Recency: the last response is favored |
| U-shaped | Both, as in Murdock's lists |

## Status

The design is settled; the harness is being built. Nothing has run yet beyond response-shape smoke tests.

- **Design:** [`plans/v1-design.md`](plans/v1-design.md). A wide-K follow-up (K = 8, 16, 32 on PPE Best-of-K) is in [`plans/wide-k-followup.md`](plans/wide-k-followup.md).
- **Method and reproduction:** [`docs/`](docs/), added as the harness lands.
- **Results:** [`results/`](results/), empty until runs complete.

## Repository layout

| Path | Contents |
|---|---|
| [`plans/`](plans/) | design documents |
| [`docs/`](docs/) | method, prompts, reproduction, limitations |
| [`sources/`](sources/README.md) | background papers on primacy, recency and position bias |
| `runs/` | append-only JSONL receipts of every API call, committed as they happen |
| `db/` | SQLite rebuilt from `runs/` for analysis (not committed) |
| `results/` | generated reports |

By **Kyle Wild**. Code is [MIT licensed](LICENSE); data, prompts and papers keep their [upstream terms](THIRD_PARTY_NOTICES.md).
