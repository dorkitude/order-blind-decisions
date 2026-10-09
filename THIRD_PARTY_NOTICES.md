---
title: Third-party notices
type: notices
updated: 2026-10-09
---

# Third-party notices

This repository's original code and documentation are [MIT licensed](LICENSE). That license does not cover the material below, which keeps its own terms.

| Material | Source | Terms | How it is used here |
|---|---|---|---|
| RewardBench 2 dataset | [`allenai/reward-bench-2`](https://huggingface.co/datasets/allenai/reward-bench-2) at revision `7ff08853b0d5686e79b13fda8677024f566a104a`, file `data/test-00000-of-00001.parquet` (SHA-256 `c8ec60efbd75d2f9dcba4121e6101f7a6015abc38a34e034ae2c7ae886265958`) | [ODC-By 1.0](https://opendatacommons.org/licenses/by/1-0/). The dataset card notes that responses also carry the terms of the models that generated them. | Prompts and responses appear in request bodies, frozen manifests and run receipts. Attribution: Allen Institute for AI and the RewardBench 2 authors (Malik et al., 2025). |
| RewardBench judge prompts | [`allenai/reward-bench`](https://github.com/allenai/reward-bench), `rewardbench/generative_v2.py` | Apache-2.0 | The four-way and ratings prompts are adapted; every change is listed in [`docs/prompts.md`](docs/prompts.md). |
| OpenAI Decisions adapter | [`dorkitude/decision-model-testing`](https://github.com/dorkitude/decision-model-testing), `experiments/openai-decisions-adapter` | MIT (same author) | Imported as a Go module to translate Jev-shaped requests to the Decisions API. |
| Background papers | See [`sources/README.md`](sources/README.md) | Per paper | Only CC-licensed PDFs are committed. arXiv-default-license PDFs are fetched locally by `sources/fetch.sh`. |
| Model outputs | Jev, OpenAI Decisions, Cloudflare Clef-flash, Microsoft-Decision-1 (via OpenRouter) | Each provider's terms | Responses are recorded in `runs/` for reproducibility. |
