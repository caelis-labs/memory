# Memory Appliance Corpus Evaluation

Status: current privacy-preserving multi-round receipt evaluation plus the G4
input boundary. It measures the model-free durable baseline; it is not a
substitute for external-Worker model or semantic-quality evaluation.

## Input boundary

`scripts/corpus_eval` accepts either a local Markdown memory file or a Codex
Session JSONL. Markdown fenced code is excluded. JSONL extraction accepts only
user `input_text` and assistant `output_text` message items; developer messages,
reasoning, tool calls, and tool outputs are excluded.

Source text, generated queries, receipt IDs, and data-directory paths never
enter the report. The report contains only source format, byte count, source
SHA-256, extraction counts, selected-query collision shape, aggregate durability
and retrieval rates, leakage counts, cohort ages, and latency percentiles. With
no `-data-dir`, the appliance database is created in a fresh temporary directory
and removed on exit. Raw source or a retained evaluation database must never be
committed to this repository.

## Multi-round method

The evaluator derives one rare lexical token per eligible source chunk, then:

1. writes one batch into a private Space;
2. measures immediate retrieval within the fixed eight-fragment response budget;
3. closes and reopens the durable Store;
4. proves every prior receipt still exists through the authorized status API,
   then Recalls it using its persisted consistency token;
5. runs the same queries through a second identity's disjoint private View and
   counts any evidence leak;
6. reports durable receipt rate, Retrieval@8, Recall@1, Recall@5, latency, and
   final results by insertion-round age.

This exercises realistic corpus shape, accumulation, restart, consistency, and
authorization behavior while keeping evaluation deterministic. Queries are
lexical and evidence-bound. Paraphrase quality, model proposals, consolidation,
and semantic ranking require a separately frozen appliance prompt-policy
profile, downstream Generator/model, and labeled corpus.

The checked-in realistic Chinese/mixed suite supplements the marker corpus with
24 product-shaped receipt and semantic cases accumulated across three restarts.
It proves deterministic retrieval and provenance only. G4 acceptance still
requires at least 200 reviewed labeled cases and the fixed 100-Space,
100,000-receipt, 10,000-Record soak; neither a private local source nor a
synthetic marker corpus may be presented as sufficient semantic-quality
evidence.

The fixed synthetic scale and durability gate is executable now:

```sh
make ga-soak
```

It creates exactly 100 isolated private Spaces, acknowledges 100,000 receipts,
organizes 10,000 semantic Record heads through the external-Worker contract,
restarts the Store, reads every receipt status, samples every Space, rebuilds
indexes, and verifies backup/restore. The restored generation repeats the full
receipt-status scan, lexical and semantic Recall provenance probes, and
cross-Space leakage probes rather than accepting aggregate counts alone. The default report is
`dist/ga-soak-report.json`; it contains only counts, environment identity,
durations, byte sizes, and health flags. Synthetic scale evidence and the
separate reviewed 200-case quality corpus are both required because neither can
substitute for the other.

## Local commands

Evaluate a Codex memory registry without retaining its contents:

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/MEMORY.md \
  -format markdown -rounds 6 -limit 400 \
  -output /private/path/memory-corpus-report.json
```

Evaluate one Codex Session export:

```sh
GOWORK=off go run ./scripts/corpus_eval \
  -source /absolute/path/to/session.jsonl \
  -format codex-jsonl -rounds 8 -limit 800 \
  -output /private/path/session-corpus-report.json
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
