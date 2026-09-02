# Real Corpus and Local Gemma Steward Evidence — 2026-09-02

Status: exploratory implementation evidence. This accepts the local Ollama
`ModelGenerator` path and one prompt safety correction. It does not accept
Steward semantic quality for GA or replace the reviewed 200-case gate.

## Frozen inputs

- source: one private local Memory registry;
- source SHA-256:
  `002552c604d1fc54c3706485ab065f408ae8de83cc92c46ca2d0262fcd1c4ed1`;
- source size: 699,154 bytes, 6,609 extracted lines, 1,056 duplicates, and
  2,723 skipped lines;
- static cases: 2,000 in eight 250-receipt rounds with restart after every
  round;
- Steward cases: the same first 12 selected inputs in the same order for both
  runs;
- model: local Ollama 0.33.2 `gemma4:12b-mlx`, digest
  `117d0d84cf2ab865feb59afc2cd30ff5d55f0035e05eb8d1b814f9688e3f3671`;
- provider envelope: schema-free JSON object mode, temperature 0, 32,768-token
  context, and 512-token generation ceiling;
- native JSON Schema: not used. Memory's system prompt contains the complete
  allowed fields and exact operation shapes;
- reports contain no source text, query, receipt identifier, model response,
  data-directory path, endpoint, or credential.

## Static 2,000-case result

| Metric | Fixed `gse` + Han fallback | SQLite `unicode61` control | Difference |
| --- | ---: | ---: | ---: |
| Retrieval@8 | 99.80% | 95.95% | +3.85 pp |
| Recall@1 | 75.40% | 70.40% | +5.00 pp |
| Recall@5 | 98.75% | 95.00% | +3.75 pp |
| MRR | 0.8574 | 0.8125 | +0.0449 |
| Zero-result rate | 0% | 3.60% | -3.60 pp |

Every acknowledged receipt survived restart and no receipt crossed into the
disjoint private View. Final-round p95 was 0.683 ms for Remember and 1.533 ms
for Recall on this developer machine. These are descriptive measurements, not
hard workstation gates.

Corpus growth makes the ranking pressure visible instead of hiding it behind a
single final ratio:

| Cumulative receipts | Retrieval@8 | Recall@1 | Recall@5 | Private leaks |
| ---: | ---: | ---: | ---: | ---: |
| 250 | 100% | 86.00% | 100% | 0 |
| 500 | 100% | 82.40% | 99.40% | 0 |
| 1,000 | 99.80% | 79.40% | 98.90% | 0 |
| 1,500 | 99.80% | 77.33% | 98.87% | 0 |
| 2,000 | 99.80% | 75.40% | 98.75% | 0 |

The fixed analyzer materially beats the legacy control while remaining stable
at Recall@5 as the corpus grows. Recall@1 declines with ranking competition, so
future ranking work still has a measurable target.

## Steward prompt trajectory

Only one deliberate parameter changed between the two runs. The tuned prompt
adds: every non-IGNORE response must copy `input.receipt.receipt_id` exactly
into `evidence_refs`. Corpus, order, model, JSON envelope, generation options,
and non-prompt Memory code remained fixed.

| Metric | Baseline prompt | Explicit receipt rule |
| --- | ---: | ---: |
| Profile prompt SHA-256 | `6e16888f...32f` | `57f0b119...5e5` |
| Completed / failed jobs | 10 / 2 | 12 / 0 |
| Valid proposal rate | 83.33% | 100% |
| Successful operations | ADD 6, IGNORE 2, MERGE 1, SUPERSEDE 1 | ADD 12 |
| Active Record heads | 6 | 12 |
| Target semantic Retrieval@8 | 33.33% | 33.33% |
| Target semantic Recall@1 | 0% | 25.00% |
| Prompt / completion tokens | 11,711 / 1,067 | 16,452 / 1,084 |
| Generation p50 / p95 | 12.169 s / 19.018 s | 13.032 s / 18.863 s |
| Wall time | 135.230 s | 165.328 s |

All 24 model calls across the two completed runs returned at the provider
boundary. The two baseline failures were appliance proposal rejections, not
provider failures. A prior aborted discovery run also demonstrated that plain
text mode could exceed the two-minute call timeout; the bounded JSON object
envelope removed that instability without supplying a JSON Schema.

The explicit receipt rule is retained because provenance is non-negotiable and
the tuned sample reached 100% protocol validity. The same result is not evidence
of better consolidation: it produced twice as many active heads as the
baseline, increased total tokens by 37.2%, and did not improve target semantic
Retrieval@8. Conservative ADD is safer than an invalid merge, but the model
sample does not yet justify automatic or default Steward execution.

The semantic probe reuses automatically selected lexical terms from the source;
it is useful for checking whether a generated Record remains evidence-linked
and searchable, but it is not a labeled paraphrase benchmark. The 33.33%
semantic target rate therefore identifies a real weakness without estimating
general semantic Recall quality.

## Product decision

- Keep the fixed, zero-token analyzer as the default product path.
- Keep Steward opt-in and demand-driven; elapsed idle time must never spend a
  token.
- Retain the explicit receipt rule and appliance-side proposal validation.
- Do not claim consolidation or pseudo-semantic quality from this sample.
- Before GA, run the current Memory-owned prompt/parser on the reviewed
  non-literal fixture and then on at least 200 labeled cases covering IGNORE,
  MERGE, SUPERSEDE, contradiction, and unrelated noise. Report proposal
  validity, operation correctness, semantic lift, active-head growth, and token
  growth together.

## Reproduction

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/MEMORY.md \
  -format markdown -rounds 8 -limit 2000 \
  -steward-model gemma4:12b-mlx -steward-limit 12 \
  -steward-timeout 1m \
  -output /private/path/memory-corpus-gemma4.json
```

The raw report remains outside the repository so an ad-hoc developer-machine
run is not mistaken for a frozen release fixture. Reproduction requires
deliberate local access to the private source and a loopback Ollama service.
