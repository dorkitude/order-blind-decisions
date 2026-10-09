#!/usr/bin/env bash
# Re-download every source PDF from arXiv and verify it against SHA256SUMS.
set -euo pipefail
cd "$(dirname "$0")"
while IFS='|' read -r id name; do
  [ -f "$name.pdf" ] || curl -fsSL -o "$name.pdf" "https://arxiv.org/pdf/$id"
done <<'LIST'
2102.09690v2|2021-zhao-calibrate-before-use
2104.08786v2|2021-lu-fantastically-ordered-prompts
2305.08845v2|2023-hou-llms-zero-shot-rankers
2305.17926v2|2023-wang-llms-not-fair-evaluators
2306.05685v4|2023-zheng-judging-llm-as-a-judge
2307.03172v3|2023-liu-lost-in-the-middle
2308.11483v1|2023-pezeshkpour-option-order-sensitivity
2309.03882v4|2023-zheng-not-robust-mc-selectors
2309.17012v3|2023-koo-cognitive-biases-evaluators
2310.01432v3|2023-li-split-and-merge
2310.07712v2|2023-tang-permutation-self-consistency
2310.13206v2|2023-wang-primacy-effect-of-chatgpt
2311.03839v3|2023-janik-human-memory-and-llms
2406.07791v9|2024-shi-judging-the-judges-position-bias
2406.15981v1|2024-guo-serial-position-effects
2406.16008v2|2024-hsieh-found-in-the-middle-calibration
2510.10276v1|2025-salvatore-lost-in-middle-emergent
LIST
sha256sum -c SHA256SUMS
