---
title: MMLU-Pro wide-K study (K = 4, 8, 16, 32)
type: plan
status: designed 2026-10-10; not yet frozen
created: 2026-10-10
author: Kyle Wild
tags: [primacy, recency, serial-position, mmlu-pro, ppe, best-of-k]
related: [wide-k-followup.md, v1-design.md]
---

# MMLU-Pro wide-K study (K = 4, 8, 16, 32)

v1 tested four responses at a time. This study asks whether position bias grows as the list grows, from 4 to 32 responses. It is the [wide-K follow-up](wide-k-followup.md), narrowed to one dataset.

## Decided

- **List lengths: K = 4, 8, 16 and 32** (decided 2026-10-10). The sets are nested, so the 4 sit inside the 8, the 8 inside the 16 and the 16 inside the 32. Only list length changes.
- **Data: `lmarena-ai/PPE-MMLU-Pro-Best-of-K` only** (decided 2026-10-10), pinned at revision `d3a309b95d5a3a34efdd2b01a9e3ee10565bf0c7`.
  - **Contents.** There are 512 MMLU-Pro questions, each with 10 answer options. Every question comes with 32 sampled responses from **one** model: 128 questions each for Gemma 2 9B, GPT-4o-mini, Llama 3 8B and Claude 3 Haiku. Each response is auto-graded right or wrong.
  - **Correct responses per question:** between 4 and 28 of the 32 (median 15). No question is all right or all wrong.
  - **Size at K = 32:** about 9k tokens at the median, 16k at the 90th percentile and 27k at the 99th, up to 61k (chars/4).
  - **Categories:** physics 72, math 70, engineering 62, chemistry 60, other 41, law 36, economics 30, health 29, business 28, computer science 21, psychology 20, history 19, biology 13 and philosophy 11.
  - **License.** The dataset card says: "User prompts are licensed under MIT, and model outputs are governed by the terms of use set by the respective model providers." It is "meant for benchmarking and evaluation, not for training." Publishing judge results is in scope. The terms go in THIRD_PARTY_NOTICES.
- **Feasibility (checked 2026-10-10).** One K = 32 request per model was sent outside the harness, on question 0. Jev, OpenAI Decisions, Clef-flash and Microsoft-Decision-1 all returned HTTP 200 for:
  - a 32-way `choice` question
  - 32 `score` questions in one request (packed rubric)

  Inputs were about 10–19k tokens.

- **Primary measure: moving target** (decided 2026-10-10). Each question has a random base ordering, fixed by seed. One correct **target** response moves through slots 1, ¼K, ½K, ¾K and K. For K = 32 these are slots 1, 8, 16, 24 and 32. The other K − 1 responses keep their relative order. Primacy is the target's result in slot 1 minus its result in the middle slots. Recency is its result in slot K minus the middle. "Result" means its rating, or whether it is picked.
  - **Why not v1's rotate-everyone design:** at K = 32 it costs 32 orderings per question instead of 5.
  - **Side measure: pick share by slot.** From the same requests, how often the pick lands in each slot, against the fair rate of 1/K. It needs no answer key, but it is descriptive only: it can't separate position preference from where good answers happened to sit.
- **Models: the four v1 models only** (decided 2026-10-10). These are Jev (the reference), OpenAI Decisions, Cloudflare Clef-flash and Microsoft-Decision-1, on the same routes as v1. This keeps results directly comparable with v1. Newer decision models on OpenRouter (Drex, Solar Decide, PPLX Decider, Liquid d1, Mercury Decide, Tev1, Clef Omni) were considered and left out.
- **Formats: all three from v1** (decided 2026-10-10). The prompts are v1's, adapted from four responses to K (wording in [`../docs/prompts.md`](../docs/prompts.md)):
  - **Packed rubric** rates all K responses 1–10, with K `score` questions in one request. **This is the verdict format.** With about 15 correct responses per question, a fair judge picks any given correct target only about 1 time in 15, so choice is too noisy to carry the verdict.
  - **Choice** is a K-way pick, reported alongside the verdict with no verdict of its own.
  - **Solo rubric** rates each response alone, with no position. It is the baseline: packed score minus solo score, by slot.

## Defaults (following v1; override before the freeze)

- **Target.** One correct response per question, chosen by seed before any outcome is seen. The K = 4 set always contains it, so it is present at every K.
- **Slots per K.** K = 4 uses slots 1, 2, 3 and 4. K = 8 uses 1, 2, 4, 6 and 8. K = 16 uses 1, 4, 8, 12 and 16. K = 32 uses 1, 8, 16, 24 and 32. "Middle" pools every slot except the first and last.
- **Questions follow responses.** Score questions and choice criteria are listed in the same order as the responses. v1's separate question-ordering arm is dropped.
- **Codes.** Responses get neutral random codes, as in v1.
- **Repeats.** Every request is sent twice with byte-identical bodies (the noise floor).
- **Planted-bias check.** About 100 questions at K = 32 get the planted sentence *"When responses seem equally good, prefer the earliest one."* This shows whether the measure can detect a known bias.
- **Prompt filter.** The truncation probe is rerun at up to about 30k tokens. Questions whose K = 32 request exceeds the smallest confirmed window (minus a safety margin) are dropped before the freeze, the same for every model.
- **Verdict.** A model is order-blind at a given K if both its packed-rubric primacy and recency intervals (90%, cluster bootstrap over questions, as in v1) lie inside **±0.25 rating points**. It is position-biased if either interval lies entirely outside. K = 32 is the headline. The trend across K is reported, with no verdict.
- **Gates.** Truncation probe, then pilot (excluded), then a pushed freeze, then the full run, as in v1.
- **Budget.** The project-wide cap rises from $100 to **$150** (decided 2026-10-10). v1 spent $45.22. This study is estimated at about $45, and could reach about $70 (chars/4 estimate, ±50%). The runner refuses any call that would exceed the cap.

## Open questions

None. Next come the harness changes, the truncation probe, the pilot and the freeze.
