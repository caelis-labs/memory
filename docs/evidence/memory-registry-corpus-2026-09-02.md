# Local Memory Registry Corpus Evidence — 2026-09-02

Status: privacy-preserving lexical product evidence and a pre-convergence
private-lexicon parameter study. This supersedes the earlier 400-case format-1
run. It is not a semantic-model quality claim or a GA sign-off.

## Frozen source and method

- source kind: local Codex Memory Markdown registry;
- source SHA-256: `24e2ffbace81bece1ee6ce2b3d84437c8575103f1d98e30c8029e8a221a238cf`;
- source bytes: 702,218;
- extracted lines: 6,625;
- evaluated cases: 2,000;
- insertion rounds: 8, with a durable Store restart after every round;
- selected queries: 955 unique and 1,045 colliding cases, maximum document
  frequency 76;
- report format: 2;
- raw source, selected text, queries, receipt IDs, paths, and evaluation
  databases retained in the repository: no.

The tested retrieval policy used fixed `gse` `zh_s` segmentation, Han two- and
three-rune fallback terms, first-term weight 2, n-gram weight 0.25, exact-phrase
weight 4, private-lexicon weight 3, and BM25 tie-break weight 0.001. The legacy
control indexed the exact same receipt texts and queries with SQLite FTS5
`unicode61`.

## Main result

| Metric | Current | Legacy `unicode61` | Absolute change |
| --- | ---: | ---: | ---: |
| Durable receipt rate | 100% | n/a | n/a |
| Retrieval@8 | 99.80% | 95.95% | +3.85 pp |
| Recall@1 | 75.45% | 70.45% | +5.00 pp |
| Recall@5 | 98.75% | 95.05% | +3.70 pp |
| MRR | 0.8578 | 0.8129 | +0.0449 |
| Zero-result rate | 0% | 3.60% | -3.60 pp |
| Cross-private-Space leakage | 0 | n/a | required zero |

All 2,000 acknowledged receipts survived every restart. No cohort fell below
98.0% Recall@5, and all eight cohorts retained at least 99.6% Retrieval@8 after
later insertions. The declining Recall@1 as the corpus grows is attributable to
real lexical collisions, not acknowledged receipt loss.

## Parameter and lexicon growth trajectory

The default local lexicon policy requires three independent receipts, two
distinct left and right boundaries, score 6, and at most eight occurrences per
receipt. Its cumulative growth was:

| Receipts | Candidates | Evidence links | Active terms | Generation sum | Recall@1 | Recall@5 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 250 | 693 | 694 | 0 | 2 | 86.00% | 100.00% |
| 500 | 1,310 | 1,474 | 0 | 2 | 82.40% | 99.40% |
| 750 | 1,653 | 1,982 | 0 | 2 | 81.07% | 99.07% |
| 1,000 | 2,121 | 2,523 | 1 | 3 | 79.40% | 98.90% |
| 1,250 | 2,933 | 3,446 | 2 | 4 | 79.28% | 98.88% |
| 1,500 | 3,379 | 4,069 | 4 | 6 | 77.33% | 98.87% |
| 1,750 | 3,870 | 4,576 | 4 | 6 | 76.40% | 98.91% |
| 2,000 | 4,790 | 5,545 | 9 | 11 | 75.45% | 98.75% |

Every round ended with zero pending index rebuilds. A separate nine-query,
30-relevant-receipt lexicon-shaped probe reached Recall@1 and Recall@5 of 100%,
MRR 1.0, mean Precision@5 0.6667, and zero leakage. That result proves the
activated terms remain searchable; it does not prove activation improved the
ranking because the static fallback could already retrieve those literals.

The same source and query set produced this A/B result:

| Profile | Minimum documents / boundaries / score | Active terms | Recall@1 | Recall@5 | MRR |
| --- | --- | ---: | ---: | ---: | ---: |
| Permissive | 2 / 1 / 4.5 | 131 | 75.45% | 98.70% | 0.85776 |
| Default | 3 / 2 / 6 | 9 | 75.45% | 98.75% | 0.85780 |
| Strict | 4 / 2 / 7 | 3 | 75.45% | 98.75% | 0.85780 |
| No activation | 100,000 / 2 / 6 | 0 | 75.45% | 98.75% | 0.85780 |

## Decision

The fixed Chinese analyzer and fallback projection are measurably effective
against the previous lexical path. The private adaptive lexicon is not yet
measurably effective on this corpus: default and strict activation tie the
no-activation control, while permissive activation is slightly worse at
Recall@5.

Therefore adaptive activation is disabled in the product runtime. Its code and
parameter evidence remain available only through an explicit internal
evaluation option until a frozen scenario shows positive lift without a
regression. Semantic and abstract-query improvement belongs to task briefing
and evidence-backed organization, not to progressively looser token activation.

## Reproduction

The commands and input controls are documented in
[Memory Appliance Corpus Evaluation](../memory-appliance-evaluation.md). Compare
reports only when source digest, extraction and query policy, round count,
limit, analyzer, ranking weights, Go version, and hardware class match.

## Caelis Session JSONL supplementary run

The format-1 evaluator previously ran against one canonical Caelis Session log:
source SHA-256
`28d2ccf9b36d83589478adcc2f8e40f5384366243eeb5ce98357bee1d47d510a`,
14,891,657 bytes, 33 extracted lines, and 17 eligible cases. Its six-round
durability, Retrieval@8, Recall@1, and Recall@5 were all 100%, with zero private
leakage. This remains only a production-shape extractor check because the
eligible sample is too small for a ranking claim.
