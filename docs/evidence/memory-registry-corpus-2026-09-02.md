# Local Memory Registry Corpus Evidence — 2026-09-02

Status: privacy-preserving product evidence for the static receipt/lexical path.
This is not a semantic-model quality claim or a GA sign-off.

## Source boundary

- source kind: local Codex Memory Markdown registry;
- source SHA-256: `24e2ffbace81bece1ee6ce2b3d84437c8575103f1d98e30c8029e8a221a238cf`;
- source bytes: 702,218;
- extracted lines: 6,625;
- evaluated cases: 400;
- insertion rounds: 6, with a durable Store restart after every round;
- raw source, selected text, queries, receipt IDs, paths, and the evaluation
  database were not retained in the repository.

The evaluator selected 198 unique-query cases and 202 colliding-query cases;
maximum query document frequency was 35. These collisions are reported rather
than discarded because they expose bounded-ranking behavior under accumulated
real text.

## Result

| Metric | Result |
| --- | ---: |
| Acknowledged receipts after final restart | 400 / 400 |
| Durable receipt rate | 100% |
| Retrieval@8 | 100% |
| Recall@1 | 83.75% |
| Recall@5 | 100% |
| Cross-private-Space leakage | 0 |

Every insertion cohort retained 100% durable receipt rate, 100% Retrieval@8,
and 100% Recall@5 after all later rounds. Final Recall@1 by cohort ranged from
79.10% to 88.06%. The target evidence was therefore consistently reachable;
the observed loss was only first-position ranking under lexical collisions.

Across the six rounds, observed Remember p50 ranged from 258 to 901 microseconds
and p99 from 946 to 8,105 microseconds. Recall p50 ranged from 177 to 203
microseconds and p99 from 336 to 846 microseconds. These timings describe one
local development machine and are not portable service-level objectives.

## Interpretation

The run closes the static-path durability, bounded retrieval, restart, and
private-isolation checks for this source digest. It also identifies a concrete
quality boundary: lexical collisions reduce first-rank precision as the corpus
grows even though evidence remains inside the five-fragment budget.

Semantic consolidation, paraphrase retrieval, contradiction handling, and
supersession quality still require the separately frozen reviewed corpus and an
explicit downstream Steward model. They must not be inferred from this result.

## Reproduction

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/MEMORY.md \
  -format markdown -rounds 6 -limit 400 \
  -output /private/path/report.json
```

Compare results only when the source digest, extraction rules, round count,
limit, Go version, and hardware class match.

## Caelis Session JSONL supplementary run

The same evaluator was run against one canonical Caelis Session event log after
adding a dedicated `caelis-jsonl` extractor. It accepts only canonical
user/assistant text parts and excludes reasoning, transient output, tool calls,
and tool results.

- source SHA-256: `28d2ccf9b36d83589478adcc2f8e40f5384366243eeb5ce98357bee1d47d510a`;
- source bytes: 14,891,657;
- extracted lines: 33;
- eligible unique cases: 17;
- insertion rounds and restarts: 6;
- final durable receipt rate, Retrieval@8, Recall@1, and Recall@5: 100%;
- cross-private-Space leakage: 0;
- raw Session data and evaluation database retained: no.

This Session is useful as a format and production-shape check, but its 17
eligible cases are too small to support a ranking or semantic-quality claim.
