# Memory Appliance Corpus Evaluation

Status: current privacy-preserving multi-round receipt evaluation plus an
opt-in local Ollama Steward sample. The default measures the model-free durable
baseline. The optional sample exercises the real `ModelGenerator` prompt,
parser, jobs, and semantic projection on private local facts; it is descriptive
evidence, not the reviewed semantic-quality gate.

## Input boundary

`scripts/corpus_eval` report format 3 accepts a local Markdown memory file, a Codex rollout
JSONL, or a Caelis Session event JSONL. It is an offline evaluation reader, not
part of a Memory Corpus protocol. Its format-specific extractors act as local
test producers: they convert source data into text before calling Memory. The
future Corpus evaluator likewise submits canonical source-neutral LeafRevision
requests; source parsing, filtering, and fidelity remain producer-specific
tests outside Memory. Markdown fenced code is excluded. Codex
extraction accepts only user `input_text` and assistant `output_text` message
items. Caelis extraction accepts only canonical user/assistant `text` parts.
Developer data, reasoning, transient events, tool calls, and tool results are
excluded.

Source text, generated queries, receipt IDs, and data-directory paths never
enter the report. The report contains only source format, byte count, source
SHA-256, extraction counts, selected-query collision shape, aggregate durability
and retrieval rates, leakage counts, cohort ages, lexicon growth, retrieval
parameters, and latency percentiles. When explicitly enabled, it also records
only Steward policy hashes, model name, job/operation/failure counts, token
counts, generation latency, and evidence-based semantic retrieval aggregates. With
no `-data-dir`, the appliance database is created in a fresh temporary directory
and removed on exit. Raw source or a retained evaluation database must never be
committed to this repository.

## Multi-round method

The evaluator derives one rare lexical probe per eligible source chunk. ASCII
probes are whole tokens; continuous Han text contributes bounded two- through
four-rune substrings so a full Chinese sentence cannot accidentally become the
query. It then:

1. writes one batch into a private Space;
2. measures immediate retrieval within the fixed eight-fragment response budget;
3. closes and reopens the durable Store;
4. proves every prior receipt still exists through the authorized status API,
   then Recalls it using its persisted consistency token;
5. runs the same queries through a second identity's disjoint private View and
   counts any evidence leak;
6. reports durable receipt rate, Retrieval@8, Recall@1, Recall@5, MRR,
   zero-result rate, latency, and final results by insertion-round age;
7. rebuilds the same corpus and probes in an in-memory SQLite FTS5
   `unicode61` index as the legacy control;
8. records private lexicon candidates, active terms, evidence links,
   generations, and pending rebuilds after every round only when the explicit
   `-experimental-lexicon` option is present; otherwise these remain zero.

This exercises realistic corpus shape, accumulation, restart, consistency, and
authorization behavior while keeping evaluation deterministic. Queries are
lexical and evidence-bound. Paraphrase quality, model proposals, consolidation,
and semantic ranking require a separately frozen appliance prompt-policy
profile, downstream ModelGenerator/model, and labeled corpus.

The checked-in release corpus is the reproducible static-path candidate gate.
It expands three digest-frozen, de-identified fixture batches into 224 cases:
64 Chinese, 64 English, and 96 spanning Spanish, French, German, Japanese,
Korean, and Arabic. Four durable rounds with intervening restarts gate Recall
quality, provenance, zero results, and a deliberately broad 750ms
user-perceived p95 budget. Adversarial records prove that both Space and exact
LabelSet partitions are enforced before retrieval, receipt-status lookup, and
consistency-token use. Run it with:

```sh
make corpus-gate
```

The 24-case realistic Chinese/mixed suite continues to cover semantic Record
provenance. Neither deterministic lexical suite is evidence of paraphrase,
model quality, the Corpus protocol, or projection behavior. Any optional
Steward semantic-quality claim still requires at least 200 reviewed cases. The
fixed 100-Space, 100,000-receipt, 10,000-Record soak remains a separate v0.5 GA
gate; neither evidence set substitutes for the other.

The optional Steward adapter accepts only an HTTP loopback Ollama origin. It
ignores `GenerationRequest.JSONSchema`, uses only Ollama's schema-free JSON
object envelope, and sends the complete Memory-owned field contract as the
system prompt followed by the bounded structured input. This
keeps Provider ownership out of the package core and prevents an evaluator
configuration mistake from sending a private corpus to a remote endpoint.

The fixed synthetic scale and durability gate is executable now:

```sh
make ga-soak
```

It creates exactly 100 isolated private Spaces, acknowledges 100,000 receipts,
organizes 10,000 semantic Record heads through the Steward Worker contract,
restarts the Store, reads every receipt status, samples every Space, rebuilds
indexes, and verifies backup/restore. The restored generation repeats the full
receipt-status scan, lexical and semantic Recall provenance probes, and
cross-Space leakage probes rather than accepting aggregate counts alone. The default report is
`dist/ga-soak-report.json`; it contains only counts, environment identity,
durations, byte sizes, and health flags. Synthetic scale evidence and the
separate reviewed 200-case quality corpus answer different claims; only the
scale report gates the flat v0.5 release, while the reviewed corpus gates any
Steward semantic-quality claim.

## Local commands

Evaluate a Codex memory registry without retaining its contents:

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/MEMORY.md \
  -format markdown -rounds 8 -limit 2000 \
  -output /private/path/memory-corpus-report.json
```

Run a bounded local-model sample against the same real corpus. The static
2,000-case path still runs unchanged; only the first selected 24 facts are sent
through Steward:

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/MEMORY.md \
  -format markdown -rounds 8 -limit 2000 \
  -steward-model gemma4:12b-mlx -steward-limit 24 \
  -output /private/path/memory-corpus-gemma4.json
```

The local-model sample answers protocol and real-corpus behavior questions:
whether jobs complete, which operations are proposed, how many Record heads
remain, whether prompt instructions drift, and whether each selected fact is
still reachable through a semantic fragment. It does not create non-literal
ground-truth queries, so it cannot by itself establish semantic Recall lift.

For an A/B parameter sweep, keep every input argument fixed and vary only the
private lexicon policy. Ordinary evaluation is the product control and leaves
the experiment disabled. Experimental profiles require explicit opt-in:

```sh
# Product control: fixed analyzer, no adaptive lexicon.
GOWORK=off go run ./scripts/corpus_eval -source /absolute/path/to/MEMORY.md \
  -rounds 8 -limit 2000 -output /private/path/default.json

# Experimental default: 3 documents, 2 boundary variants, score 6.
GOWORK=off go run ./scripts/corpus_eval -source /absolute/path/to/MEMORY.md \
  -rounds 8 -limit 2000 -experimental-lexicon \
  -output /private/path/experimental-default.json

# Experimental permissive.
GOWORK=off go run ./scripts/corpus_eval -source /absolute/path/to/MEMORY.md \
  -rounds 8 -limit 2000 -experimental-lexicon \
  -lexicon-min-docs 2 -lexicon-min-boundaries 1 \
  -lexicon-min-score 4.5 -output /private/path/permissive.json

# Experimental strict.
GOWORK=off go run ./scripts/corpus_eval -source /absolute/path/to/MEMORY.md \
  -rounds 8 -limit 2000 -experimental-lexicon \
  -lexicon-min-docs 4 -lexicon-min-boundaries 2 \
  -lexicon-min-score 7 -output /private/path/strict.json
```

Evaluate one Codex Session export:

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/session.jsonl \
  -format codex-jsonl -rounds 8 -limit 800 \
  -output /private/path/session-corpus-report.json
```

Evaluate one canonical Caelis Session event log:

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/session.events.jsonl \
  -format caelis-jsonl -rounds 8 -limit 800 \
  -output /private/path/caelis-session-corpus-report.json
```

Compare only aggregate reports produced from the same source digest, extraction
rules, round count, limit, Go version, and hardware class. Durable receipt rate
proves post-restart storage survival independently of ranking. Retrieval@8 says
whether the target evidence is present within the bounded response; Recall@1
and Recall@5 describe its ranking position. Selected-query collision counts and
maximum document frequency make ambiguous lexical probes visible instead of
mislabeling budget misses as data loss. A durable receipt rate of one and zero
private leaks are hard requirements. Retrieval, ranking, and latency remain
descriptive until a specific corpus digest and query policy are frozen as a
release gate.

An increasing candidate or active-term count is not a quality result. A local
lexicon profile is acceptable only when it beats the static control on the same
frozen probes without increasing private leakage or zero-result rate. If no
profile does, the report must say so and the lexicon remains an experimental
internal mechanism rather than a claimed product improvement. Lexicon tuning
flags without `-experimental-lexicon` fail instead of silently changing the
product-control path.

The first retained privacy-preserving result for the package path is
[Local Memory Registry Corpus Evidence](evidence/memory-registry-corpus-2026-09-02.md).
The separate downstream-model parameter and replication evidence is
[Memory Steward Evaluation](evidence/memory-steward-evaluation-2026-09-02.md).
The current prompt/parser path and local Gemma real-corpus sample is
[Real Corpus and Local Gemma Steward Evidence](evidence/memory-real-corpus-gemma4-2026-09-02.md).

## Product-quality evaluation ladder

Receipt or Leaf reachability is necessary but does not prove that a hierarchy
improves retrieval. Post-v0.5 Corpus experiments therefore use a frozen tuning
partition and blind holdout partition and isolate at least these conditions:

1. the current Fact Memory receipt/Record Recall control;
2. direct lexical search over authorized committed Items;
3. direct seeds plus collapsed cross-level deterministic artifacts;
4. controlled expansion from deterministic rollup artifacts;
5. the same candidate strategy with optional model-proposed summaries and
   cited expansion terms.

This ordering prevents a direct-index gain from being attributed to a tree and
does not assume root-first traversal. Every report includes all tried topology,
candidate, and expansion parameters, not only the winner. It records dataset,
producer, protocol, schema, topology, analyzer, index, model, summary, and query
policy revisions; Leaf request digests; candidate and byte budgets; projection
generation and coverage; latency; storage; rebuild time; and model-call/token
cost. A tuning gain that disappears on the blind holdout is rejected evidence.

The Corpus and projection metrics are deliberately broader than
question-answer retrieval:

- Leaf request fidelity, ordered-Item and content-digest integrity, revision
  conflicts, consistency, and replay idempotency;
- direct Item index rebuild equality and usefulness with optional projections
  disabled;
- manifest completeness, deterministic structural/index artifact digests, and
  generation activation atomicity;
- summary assertion and expansion-entry support coverage;
- stale-artifact exclusion after Leaf revision, retraction, redaction, erasure,
  or projection-policy change;
- Retrieval@k, Recall@k, MRR, zero-result rate, candidate artifacts, expansion,
  bytes, and result-budget truncation;
- correction, supersession, contradiction, repeated support, abstention,
  harmful context, private leakage, and cross-partition reference errors;
- materializer/model calls and tokens per generation, latency, database growth,
  and recovery time.

Long-conversation or memory QA benchmarks may be used as secondary comparison
sets. They cannot substitute for local product cases because conversational QA
does not by itself test the public Corpus contract, projection authority,
stale-artifact invalidation, rebuild, or identity-view boundaries. A downstream Session
projector separately owns tests proving that one concrete Session JSONL becomes
one correct admitted leaf; those tests do not change Memory semantics.

Embedding, graph, and adaptive-lexicon experiments use the same
partitions and budgets. They remain non-default unless blind-holdout task
benefit is material after operational cost and false-positive regressions are
included, and the report names a deterministic rollback to the prior accepted
projection.

The dated evidence reports linked above remain historical flat/Steward
evidence. They must not be cited as proof of the Corpus protocol or any
projection acceptance.
