# M06 facts longitudinal evaluation fixtures

Status: authored, de-identified, deterministic evaluation fixtures for the M06
facts/evidence surface. They are **not** human-reviewed gold and contain no
private source text, personal identifiers, credentials, or product types.

## Files

| File | Purpose |
| --- | --- |
| `manifest.json` | frozen thresholds, controlled alias dictionary, fixture SHA-256 identities, candidate expansion identity, review status, release blocker |
| `trajectories.json` | 14 authored blocking longitudinal trajectories |
| `candidates.json` | 26 candidate templates × 8 subjects = 208 machine-expanded candidates |

## Blocking set

`trajectories.json` is the deterministic gate. Each trajectory runs against its
own temporary on-disk SQLite database through real `Store` facts, evidence and
management APIs with a fixed clock. It covers:

- exact Space and LabelSet isolation, including a same-Space different-LabelSet
  read and a shared-view read;
- durable restart, source-identity idempotency, content conflict, and reused
  effect identity;
- temporary exception with a finite interval, `as_of` behavior, override of the
  exact base record, and denial of the exception;
- permanent change with an explicit effective time, stale-revision rejection,
  and pre-onset rejection;
- correction with preserved revision history;
- denial and exclusion from current reads;
- forget via `DeleteReceipt`: completed managed cleansing, cleared derived
  payload, source suppression, and no resurrection after `RebuildFTS`;
- mutation subject mismatch, missing/stale target, and untrusted role;
- owner ingestion policy denial and lifting;
- multi-mutation batch atomicity and effect-identity non-consumption;
- mixed-language explicit preference and controlled aliases;
- historical onset and read-budget behavior;
- recall-only capability rejection on the evidence write path.

The blocking set is a structural conformance suite. It says nothing about
paraphrase, extraction, model or consumer-answer quality. Any repository edit to
`trajectories.json` or `candidates.json` must be re-hashed into `manifest.json`;
the gate refuses a mismatch.

## Candidate set

`candidates.json` expands to 208 trajectories that each establish a preference
as a user quote, confirm it, optionally change it, and then read it back. Every
candidate carries `review_status = not_human_reviewed`; the manifest also
records `human_reviewed_cases = 0` and the outstanding release blocker.

The candidate set is generated from the implementation's own controlled
vocabulary (`preference.drink`, `diet`, `language`, `timezone`), so a rate
measured over it is circular candidate-tier evidence. It exists to freeze a
reproducible set that a human reviewer can later replace or confirm. It must
never be reported as human-reviewed gold.

## Reproduction

```sh
make facts-gate
```

The gate always runs the authored blocking set. It runs the candidate set when
`MEMORY_FACTS_EVAL_CANDIDATES=1` (which `make facts-gate` sets) and writes an
owner-only report to `dist/facts-eval-report.json`. `scripts/facts_eval`
independently re-verifies the manifest, digests, labels, frozen thresholds and
the emitted report:

```sh
go run ./scripts/facts_eval -fixtures internal/appliance/testdata/facts_eval \
  -report dist/facts-eval-report.json
```

## Thresholds

`explicit_preference_min = 0.95` and `recall_at8_alias_min = 0.90` are frozen
before measurement. Both the Go gate and `scripts/facts_eval` carry an
independent floor for these values, so loosening the manifest alone cannot
produce a passing result.

## Independent substitute review

The user authorized independent AI-agent review in place of manual trajectory
review. Per-case decisions for all 208 expanded candidates and 14 blocking
trajectories are retained in [candidate review](../../../../docs/evidence/memory-v0.6-agent-candidate-review.md)
and [blocking review](../../../../docs/evidence/memory-v0.6-agent-blocking-review.md).
`not_human_reviewed` and human count 0 remain truthful provenance labels. The
substitute review is complete; production model/holdout quality is still unqualified.
The runner now consumes frozen expected labels, verifies pending exclusion and
exact source attribution, and t03 checks denial inside the old exception interval.
