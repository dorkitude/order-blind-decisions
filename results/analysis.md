---
title: "Analysis: are decision models order-blind?"
type: analysis
run: main
date: 2026-10-09
updated: 2026-10-09 (Microsoft-Decision-1 added)
author: Kyle Wild
data: results/main-report.md (generated), runs/main/ (receipts)
tags: [primacy, recency, serial-position, decision-models, rewardbench-2]
---

# Analysis: are decision models order-blind?

**Short answer:** Jev is, on the preregistered test. OpenAI Decisions is not: it favors whatever comes first. Microsoft-Decision-1 is not either: it disfavors whatever comes last. Clef-flash is inconclusive, and it is far less accurate than the others.

Two exploratory caveats apply even to Jev:

- **Exact ties.** When two answers are equally correct, Jev, Decisions and Clef-flash usually pick the one shown first. Microsoft-Decision-1 is the exception, at close to 50/50.
- **Math.** All four show primacy on RewardBench 2's Math subset.

**Microsoft-Decision-1 was added after the plan was frozen.** It was announced on 2026-10-09 and appeared on OpenRouter that evening, after the other three models had finished. It ran the identical frozen plan with the same endpoints, margins and verdict rule (disclosed in the [plan](../plans/v1-design.md)).

The machine-generated numbers behind every claim here are in [`main-report.md`](main-report.md). The design, endpoints and verdict rule were frozen in [`../plans/v1-design.md`](../plans/v1-design.md) before the main run.

## What was run

```mermaid
flowchart LR
    RB[RewardBench 2<br/>1,865 prompts × 4 responses] --> F{format}
    F --> C[choice: pick the best]
    F --> P[packed rubric: rate all 4 in one request]
    F --> S[solo rubric: rate 1 alone]
    C --> A{ordering arm}
    P --> A
    A --> H[response order<br/>4 Williams orderings]
    A --> Q[question order<br/>4 Williams orderings]
    H --> X[× 2 identical repeats<br/>× 4 models]
    Q --> X
    S --> X
    X --> R[304,800 requests<br/>all answered]
```

- **Data.** Every RewardBench 2 test prompt (1,865), with four candidate responses each, labelled correct or wrong by the benchmark.
- **Models.** Each model received the same 76,200 requests:
  - Jev (`jev-1.13.0`, the reference)
  - OpenAI Decisions (`gpt-6-luna`)
  - Cloudflare Clef-flash (`clef-flash`)
  - Microsoft-Decision-1 (`microsoft/microsoft-decision-1` via OpenRouter, added after the freeze)
- **Formats.** All formats use RewardBench 2's official judge prompts, minus the sentence telling the judge to avoid position bias, and with neutral response codes instead of letters ([details](../docs/prompts.md)).
- **Ordering arms.**
  - In the **response-order** arm, each response visits every slot once, via a Williams Latin square.
  - In the **question-order** arm, the responses stay put and only the order of the questions or answer options rotates.
- **Scale.** 304,800 requests in total, 100% answered. The project cost **$43.08** at list price for reported tokens: $42.46 for the main runs plus $0.62 for the probes and pilots. That is under the $100 cap.

## Verdicts

| Model | Verdict | Choice accuracy | Primacy (slot 1 − middle) | Recency (slot 4 − middle) | Rating shift, slot 1 / slot 4 |
|---|---|---|---|---|---|
| **Jev** | ✅ **order-blind** | **80.9%** | +0.2 pp [−0.8, +1.2] | +0.2 pp [−0.7, +1.0] | +0.04 / −0.06 |
| **OpenAI Decisions** | 🟥 **position-biased** | 78.2% | **+6.1 pp [+4.7, +7.4]** | −1.5 pp [−2.7, −0.4] | **+0.26** / −0.03 |
| **Clef-flash** | 🟨 inconclusive | 62.7% | −2.1 pp [−3.5, −0.8] | +1.9 pp [+0.8, +3.0] | +0.08 / +0.12 |
| **Microsoft-Decision-1** | 🟥 **position-biased** | 70.9% | +1.0 pp [−0.2, +2.2] | **−4.5 pp [−5.6, −3.4]** | +0.09 / −0.08 |

- **Intervals.** Brackets are 90% cluster-bootstrap intervals over items.
- **Margins.** Order-blind requires every primary interval to sit inside ±3 percentage points (choice) and ±0.25 rating points on the 1–10 scale.
- **Why Decisions is not order-blind.** Its choice primacy interval lies entirely above +3 pp.
- **Why Clef-flash is inconclusive.** Its choice primacy interval crosses −3 pp: slot 1 is slightly *disfavored*.
- **Why Microsoft-Decision-1 is not order-blind.** Its choice recency interval lies entirely below −3 pp: a correct response in the last slot is chosen 4.5 points *less* often than in the middle. This is an anti-recency effect, not the usual recency.

## The serial-position curves

Murdock (1962) found that people remember the first and last items of a list best. For a judge, the analogue is the chance of choosing the correct response, plotted by the slot it appears in. A flat line is order-blind.

```mermaid
%%{init: {"themeVariables": {"xyChart": {"plotColorPalette": "#2f6fdf, #d9480f, #2b8a3e, #7048e8"}}}}%%
xychart-beta
    title "Correct answer chosen, by its slot"
    x-axis "Slot of the correct response" [1, 2, 3, 4]
    y-axis "Share chosen correctly" 0.55 --> 0.90
    line [0.811, 0.837, 0.782, 0.811]
    line [0.834, 0.759, 0.788, 0.758]
    line [0.608, 0.653, 0.607, 0.649]
    line [0.722, 0.744, 0.681, 0.667]
```

*Lines: Jev blue, OpenAI Decisions orange, Clef-flash green, Microsoft-Decision-1 purple.*

| Model | Slot 1 | Slot 2 | Slot 3 | Slot 4 | Share of picks landing in slot 1 (25% is fair) |
|---|---|---|---|---|---|
| Jev | 81.1% | 83.7% | 78.2% | 81.1% | 25.3% |
| OpenAI Decisions | **83.4%** | 75.9% | 78.8% | 75.8% | **29.9%** |
| Clef-flash | 60.8% | 65.3% | 60.7% | 64.9% | 24.3% |
| Microsoft-Decision-1 | 72.2% | 74.4% | 68.1% | **66.7%** | 26.9% |

- **Jev's line is flat**, with no rise at either end.
- **Decisions' line starts high and drops.** That is primacy, and it is about six points stronger than Jev's (Decisions − Jev: **+5.8 pp [+4.3, +7.4]**).
- **Clef-flash's line zigzags**, with slots 2 and 4 above 1 and 3. That is neither a clean primacy nor a clean recency pattern.
- **Microsoft-Decision-1's line slopes down.** The first two slots do best and the last does worst. Its picks land in slots 1–4 at 26.9 · 26.9 · 23.2 · 22.9%.

## Findings

### 1. Jev is order-blind on the preregistered test

- **All four primary endpoints are inside the margins**, by a wide berth. The choice intervals stay within ±1.2 pp of zero against a ±3 pp margin.
- **Ratings drift slightly downward** with slot: +0.04 at slot 1, −0.06 at slot 4. That is detectable at this sample size but about a quarter of the margin.
- **Its picks are spread evenly** across slots: 25.3 · 26.6 · 23.2 · 25.0%.
- **Question order barely matters to Jev.** Rotating the answer options or the rating questions moves its choices by at most 0.6 pp and its ratings by at most 0.003 points.
- **It is also the most accurate judge** here: 80.9%, against 78.2% for Decisions, 70.9% for Microsoft-Decision-1 and 62.7% for Clef-flash.

### 2. OpenAI Decisions has a clear primacy bias

- **The correct response is chosen 83.4% of the time in slot 1**, against 75.9–78.8% elsewhere. Decisions picks slot 1 29.9% of the time.
- **It rates the first response about a quarter point higher** (+0.26 on the 1–10 scale). That is +0.23 more than Jev.
- **Answer-option order matters too.** With the responses held in place, the correct answer is chosen 2.8 pp less often when it is the *last* answer option.
- **It is perfectly deterministic.** Identical requests always got identical answers, so this is systematic bias, not noise. It is consistent with Decisions' first-position failure on JudgeBench in the earlier eval-judging study.

### 3. Clef-flash is weaker, and inconclusive on order

- **It is markedly less accurate** (62.7%), and the gap is widest on Factuality (48.2% against 75.9% for Jev).
- **Its choices zigzag by slot**, and its ratings form a shallow U (+0.08 at slot 1, +0.12 at slot 4).
- **It is the only model whose ratings depend on question order** (−0.07 / +0.06).
- **It rates responses about 0.71–0.87 points lower** when they appear four to a request than when rated alone.

### 4. Microsoft-Decision-1 disfavors the last slot, and does not have "zero flips"

- **The last slot is penalized.** A correct response in slot 4 is chosen **4.5 pp less often** than in the middle ([−5.6, −3.4]), and **4.6 pp worse than Jev** ([−6.0, −3.3]). That fails the ±3 pp margin. Slot 1 is roughly neutral (+1.0 pp [−0.2, +2.2]), and its rating shifts stay well inside the margin (+0.09 at slot 1, −0.08 at slot 4).
- **Accuracy is 70.9%**, between Clef-flash and the other two. It is weakest on Precise IF (41.1%) and Factuality (65.5%).
- **It is the most order-blind on exact ties** (52.8%, see below), and it **never refused**.
- **Rating with others lowers its scores.** In a packed request it rates a response 0.19–0.35 points lower than the same response rated alone, and more so the later the response appears.

**Microsoft's claim.** Microsoft's [announcement](https://commandline.microsoft.com/microsoft-decision-1-model-foundry/) reports "zero flips" when answer options are reordered or shuffled. This study tests that directly: the question-order arm rotates only the answer options while the responses stay put.

| Pick changed across the 4 orderings (share of item × repeat sets) | Jev | OpenAI Decisions | Clef-flash | Microsoft-Decision-1 |
|---|---|---|---|---|
| Answer options reordered (question-order arm) | 6.0% | 21.8% | 2.5% | **5.1%** |
| Responses reordered (response-order arm) | 20.8% | 34.6% | 35.5% | **31.9%** |
| Retest noise: identical requests disagree | 2.1% | 0.0% | 1.4% | 2.6% |

- **Options reordered: not zero, but close to noise.** Reordering the answer options changed Microsoft-Decision-1's pick in 5.1% of sets. That is not zero, but most of it is consistent with the model's own retest noise: identical requests disagreed 2.6% of the time, and each set compares four orderings.
- **Responses reordered: far from zero.** Moving the responses themselves changed its pick in **31.9%** of sets.
- **Not the most stable either way.** On both measures Clef-flash flips less on option order, and Jev flips less on response order.

### 5. Exact ties: three models break them by position (secondary)

For RewardBench 2's Ties prompts with two equally correct answers (for example "Monday" and "Friday" for *"Select a random day of the week"*), an order-blind judge would pick the earlier-shown one 50% of the time. Three of the four models favor it:

```mermaid
%%{init: {"themeVariables": {"xyChart": {"plotColorPalette": "#2f6fdf, #868e96"}}}}%%
xychart-beta
    title "Exact ties: how often the earlier correct answer wins"
    x-axis ["Jev", "OpenAI Decisions", "Clef-flash", "Microsoft-Decision-1"]
    y-axis "Share of tie choices (%)" 0 --> 100
    bar [71.5, 88.6, 69.1, 52.8]
    line [50, 50, 50, 50]
```

*Bars: share of tie choices that went to the earlier-shown correct answer. Grey line: 50%, the order-blind expectation.*

| Model | Earlier answer wins [90% CI] | Tie choices |
|---|---|---|
| Jev | 71.5% [66.5, 76.5] | 400 |
| OpenAI Decisions | 88.6% [84.7, 92.2] | 404 |
| Clef-flash | 69.1% [63.5, 74.8] | 398 |
| Microsoft-Decision-1 | **52.8% [47.5, 58.4]** | 398 |

The design balances which correct answer comes first, so a pure preference for one answer's content would average out to 50%. **Jev ignores position when one answer is better, but uses position to break exact ties.** Decisions does so most strongly. Microsoft-Decision-1 is the only model whose interval includes 50%.

### 6. Math shows primacy for every model (exploratory)

| Subset | Jev | OpenAI Decisions | Clef-flash | Microsoft-Decision-1 |
|---|---|---|---|---|
| Factuality | −1.8 [−4.1, +0.4] | **+9.4 [+6.6, +12.1]** | −2.8 [−5.6, −0.1] | −0.1 [−2.6, +2.5] |
| Focus | −1.3 [−2.8, +0.2] | +0.8 [−1.0, +2.7] | −2.0 [−4.4, +0.3] | +0.0 [−1.8, +1.8] |
| **Math** | **+9.0 [+5.3, +12.7]** | **+30.3 [+25.4, +35.2]** | **+11.5 [+7.0, +16.1]** | **+5.7 [+1.9, +9.6]** |
| Precise IF | +3.8 [−1.6, +9.1] | −0.3 [−7.2, +6.2] | +6.4 [+0.8, +12.0] | **+12.7 [+6.9, +18.4]** |
| Safety | −0.8 [−2.4, +0.8] | +1.2 [−0.9, +3.3] | **−10.6 [−13.1, −8.2]** | −2.7 [−4.8, −0.7] |
| Ties `ref` | +1.0 [+0.0, +2.9] | +2.0 [+0.0, +4.9] | +2.0 [+0.0, +5.9] | +0.0 [−2.0, +2.0] |

Values are choice primacy (slot 1 − middle) in percentage points.

- **Math is the exception for Jev.** Its pooled verdict holds because its other subsets lean slightly the other way.
- **This is not simply difficulty.** Precise IF is harder for every model (34–49% accuracy), yet shows no comparable primacy.
- **Microsoft-Decision-1's largest primacy is on Precise IF** (+12.7 pp), its hardest subset.
- **Treat this as exploratory.** It covers seven subsets times four models with no multiple-comparison correction, and Math has only 183 prompts. It is the obvious place to look next.

### 7. Consistency

| Model | Same winner in all 4 orderings | Identical repeat gives same choice | Mean rating change between identical repeats |
|---|---|---|---|
| Jev | **79.2%** | 97.9% | 0.096 |
| OpenAI Decisions | 65.4% | **100%** | 0.000 |
| Clef-flash | 64.5% | 98.6% | 0.034 |
| Microsoft-Decision-1 | 68.1% | 97.6% | 0.086 |

Jev keeps the same winner across orderings most often. It is not fully deterministic: about 2% of identical repeat calls changed its choice. Decisions is perfectly repeatable, yet least stable when the order changes. That is the signature of a systematic position effect.

### 8. Decisions refuses to judge harmful-content prompts

- **What it refused.** OpenAI Decisions refused 338 choice requests, 1,104 packed and 344 solo, almost all in RewardBench 2's Safety subset. These are requests for private addresses and phone numbers, hacking tutorials, drug prices, and sexual content.
- **Why that matters.** A judge that refuses cannot grade the very safety behavior this subset measures.
- **Refusals depend on order.** 49 items were refused in only some orderings.
- **The other three never refused.**

Under the preregistered rule, choice refusals count as wrong answers.

### 9. The planted-bias check was weak; Decisions' natural bias does its job

- **The sentence barely moved anything.** Adding *"When responses seem equally good, prefer the earliest one"* changed primacy by about +2 to +5 pp in choice and −0.02 to +0.08 rating points across the four models, on 100 items with wide intervals. Decision models largely ignore this kind of instruction.
- **The test can still detect bias.** The same metrics cleanly separate Decisions' natural primacy (+6.1 pp) from Jev's flat line, so Jev's null result is not the test failing to see bias.

## Operations

| Model | Requests | Retries | Rate-limited | Cost (list price) | Latency p50 / p95 |
|---|---|---|---|---|---|
| Jev | 76,200 | 8 (pause cancellations) | 0 | $6.94 | 105 / 149 ms |
| OpenAI Decisions | 76,200 | 12 | 0 | $14.76 | 113 / 272 ms |
| Clef-flash | 76,200 | 8,955 | 8,947 | $14.86 | 260 / 638 ms |
| Microsoft-Decision-1 | 76,200 | 494 | 356 | $5.90 | 307 / 414 ms |

- **Clef-flash's rate limit.** Cloudflare rate-limited Clef-flash at about **1,360 successful calls per minute**. Its published Workers AI limits list the model's task type at 300 per minute and say nothing about Clef specifically, so the real limit had to be measured.
- **Microsoft-Decision-1 through OpenRouter.** It ran at 8 concurrent requests. Of its 494 retried attempts, 356 were rate-limited (HTTP 429), 130 were transient server errors (HTTP 500), and 8 were cut off when a first run session was stopped and resumed. All were retried until they succeeded. OpenRouter served it from Azure and billed $5.99 across the probe, pilot and main run, matching the list-price estimate.
- **No truncation.** State truncation was ruled out first for every model; see the [truncation probe](truncation-probe.md).
- **Units.** Costs are reported tokens × list price, not invoices. Latency is measured round trips at the run's concurrency.

## Limits

- **One benchmark, four responses.** RewardBench 2 is public, so its content may be in training data. Position effects in language models often grow with list length; the [wide-K follow-up](../plans/wide-k-followup.md) would test 8, 16 and 32 responses.
- **Adapted prompts.** The prompts are adapted for decision models. The results describe this setup, not every way these models can be used as judges.
- **Interim look.** The main run was paused once to check it was working, and endpoints were computed on partial data. Nothing in the design changed; see the disclosure in the [plan](../plans/v1-design.md).
- **Statistics.** Secondary and subset results are exploratory and not corrected for multiple comparisons.
