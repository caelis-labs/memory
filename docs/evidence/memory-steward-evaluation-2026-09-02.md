# Memory Steward Evaluation — 2026-09-02

Status: pre-GA model-assisted quality evidence for external review. This report
accepts the low-cost Steward policy for continued integration; it does not
satisfy the separate 200-case semantic GA gate.

## Frozen experiment

- downstream model: `xiaomi/mimo-v2.5` through the existing Caelis provider
  stack;
- reasoning effort: `none`;
- fixture SHA-256:
  `0d7d8b2e255f88814a23fe06056a4fc68cc2d7cc9db20ff88e16f9349cd5ada5`;
- cases: 64 receipt/query pairs in eight insertion rounds;
- groups: 30 technical aliases, 2 technical categories, 25 Chinese aliases,
  and 7 Chinese categories;
- every query is deliberately absent as a literal receipt substring;
- static and Steward modes use separate temporary Stores and the same embedded
  Memory package, binding, Recall budget, insertion order, and queries;
- reports contain only aggregate measurements, fixture and prompt hashes,
  protocol shapes, timing, and token counts. They contain no provider
  credential, receipt text, query text, receipt ID, or Store path.

The final policy is `caelis-default` version 3. Its profile prompt SHA-256 is
`6e16888f4012167d9b202877e007d50aa0f0dfa909243ecb2ad2ea1868aec32f`.
For requests without lexicon candidates, the complete effective prompt SHA-256
is `579a6f464d5e649576bf4d385ff89f943f6ba8b1d9b96d6dbbdfaf8c834a34b7`.
The bounded profile uses 16 context records, 128 KiB input, and 4 KiB output.

## Parameter history

| Stage | Deliberate change | Completed / failed jobs | Recall@1 | Recall@5 | MRR | Total tokens | Decision |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| Protocol discovery | No exact JSON-object field contract | 0 / 64 | 0% | 0% | 0 | 30,413 | Rejected: MiMo returned valid JSON with incompatible fields |
| v2 | Exact field contract; conservative alias policy | 64 / 0 | 34.38% | 34.38% | 0.3438 | 120,918 | Rejected for quality, retained as baseline |
| v3 run A | At most two controlled aliases or one immediate category | 64 / 0 | 79.69% | 85.94% | 0.8255 | 133,912 | Accepted replicate |
| v3 run B | Same model, fixture, effort, and policy | 64 / 0 | 81.25% | 87.50% | 0.8438 | 134,715 | Accepted replicate |
| Invalid lexicon variant | Mention `lexicon_terms` even with no candidates | 50 / 14 | 59.38% | 65.63% | 0.6224 | 127,076 | Rejected: 14 outputs invented terms and distracted normalization |
| v3 final | Lexicon sub-protocol only when candidates exist | 64 / 0 | 79.69% | 82.81% | 0.8086 | 134,599 | Accepted implementation evidence |

The three valid v3 full runs average 80.21% Recall@1, 85.42% Recall@5,
0.8260 MRR, 10.94% zero-result rate, and 134,409 tokens. Recall@1 ranges
only from 79.69% to 81.25%. Against v2, mean Recall@1 improves by 45.83
percentage points while token use increases by 11.16%. All 192 model calls in
the valid v3 replicates completed without a provider-call or proposal-protocol
failure.

The invalid lexicon variant is retained because it establishes an important
control: exposing an irrelevant optional concept to a low-cost model is not
free. The final bridge supplies the lexicon contract only when the appliance
has provided same-Space, evidence-backed candidates, and only exact candidate
term values are permitted. The appliance remains the validation authority.

## Final run growth trajectory

| Receipts | Completed / failed jobs | Active records | Recall@1 | Recall@5 | MRR | Zero-result rate | Cumulative tokens |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 8 | 8 / 0 | 8 | 75.00% | 75.00% | 0.7500 | 25.00% | 8,598 |
| 16 | 16 / 0 | 16 | 81.25% | 81.25% | 0.8125 | 18.75% | 23,592 |
| 24 | 24 / 0 | 24 | 87.50% | 87.50% | 0.8750 | 8.33% | 42,176 |
| 32 | 32 / 0 | 32 | 84.38% | 84.38% | 0.8438 | 12.50% | 60,787 |
| 40 | 40 / 0 | 40 | 75.00% | 77.50% | 0.7583 | 20.00% | 79,534 |
| 48 | 48 / 0 | 48 | 79.17% | 81.25% | 0.7986 | 16.67% | 98,184 |
| 56 | 56 / 0 | 56 | 76.79% | 80.36% | 0.7827 | 14.29% | 116,697 |
| 64 | 64 / 0 | 64 | 79.69% | 82.81% | 0.8086 | 12.50% | 134,599 |

The trajectory does not monotonically increase because later batches introduce
harder Chinese and category queries. Durability and job completion do increase
monotonically. The quality claim is therefore based on the frozen group mix and
replicates, not on a misleading expectation that accumulation alone raises a
ratio.

## Final group result

| Group | Cases | Recall@1 | Recall@5 | MRR | Zero-result rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| Technical alias | 30 | 83.33% | 86.67% | 0.8500 | 10.00% |
| Technical category | 2 | 50.00% | 50.00% | 0.5000 | 0% |
| Chinese alias | 25 | 80.00% | 80.00% | 0.8000 | 16.00% |
| Chinese category | 7 | 71.43% | 85.71% | 0.7500 | 14.29% |

Static Recall has 0% target Recall@8 on this deliberately non-literal semantic
fixture. This does not contradict the 2,000-case real local corpus result: that
separate experiment measures durable lexical retrieval and shows 99.80%
Retrieval@8. Together, the experiments show that the fixed Chinese analyzer
improves literal/partial lexical Recall while the optional Steward adds useful
aliases and categories that a lexical path cannot infer.

## Cost and latency

The final run used 130,320 prompt tokens, including 32,768 cached input tokens,
and 4,279 completion tokens. Mean total use across the three accepted runs was
2,100 tokens per receipt. Final per-call latency was 2.779 seconds at p50 and
4.095 seconds at p95 on the selected remote model path. These values are
descriptive user-experience evidence, not hard performance gates.

## Acceptance and remaining risks

Accepted for the pre-GA feature:

- MiMo at `effort=none` is sufficient; a higher-cost model is not required for
  the current Steward default;
- exact JSON-object instructions are mandatory because this MiMo endpoint
  constrains JSON syntax but does not enforce the supplied JSON Schema;
- controlled bilingual, abbreviation, and immediate-category expansion gives
  repeatable, material Recall lift;
- lexicon recommendations must remain candidate-gated and appliance-validated;
- an unbound Steward continues to use the zero-token static path.

Still required before semantic GA acceptance:

- expand the privacy-reviewed labeled set to at least 200 cases without
  weakening its non-literal-query rule;
- add contradiction, correction, MERGE, SUPERSEDE, stale revision, and repeated
  subject/attribute cases; the current quality fixture primarily exercises ADD;
- evaluate ranking under multiple relevant records and more unrelated noise;
- rerun the exact candidate revisions in the Rocky Linux and Caelis release
  matrix;
- complete independent external review. The reviewer should treat category
  groups, especially the two-case technical category group, as directional
  evidence rather than a stable estimate.

## Reproduction boundary

The opt-in evaluator lives with the Caelis Host integration because that is the
owner of model placement and credentials. It copies only the explicitly
selected local profile and managed credential into temporary Stores, binds the
system-managed Steward, and writes an aggregate report when
`CAELIS_REAL_MEMORY_STEWARD_REPORT` is set. The local provider Store and report
path are operator inputs and must never be committed.

