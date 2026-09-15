# Memory v0.6 Facts Structural Baseline Comparison — 2026-09-15

Status: structural, model-free comparison between the exact v0.5.2 baseline and
the current tree, produced by `scripts/facts_compare`. It measures Steward
context selection and evidence Recall fragments only. It contains no claim that
any preference was answered, adopted, extracted, paraphrased, or consumed
correctly, and no model is invoked.

Superseded label: the body below is the provisional run. Its baseline-side
Recall arm was labeled `B0` but ran after Steward organized the old coffee
Record, so it is a `B1` observation. See [Correction: superseded B0
label](#correction-superseded-b0-label) and the corrected report in
[`docs/evidence/memory-v0.6-comparison-final.json`](memory-v0.6-comparison-final.json).
The raw aggregate below is preserved unchanged.

Method and tier ownership: [`docs/memory-v0.6-evaluation.md`](../memory-v0.6-evaluation.md).

## Command

```sh
go run ./scripts/facts_compare \
  -baseline-revision 51693ff135be8c4149c15117980290aaad6d90da \
  -unrelated 80 \
  -output dist/facts-compare-report.json
```

Gated end-to-end check (archives the baseline and runs both trees):

```sh
MEMORY_FACTS_COMPARE=1 go test -count=1 -timeout 20m \
  -run '^TestFactsCompareEndToEnd$' ./scripts/facts_compare/
```

The runner extracts the exact baseline with `git archive` into the system
temporary directory, copies the current working tree, injects one fixture and
one generated test into both trees, and runs `go test` in each. No credentials,
no network, no model, no Caelis edit.

## Measured snapshot

| Item | Value |
| --- | --- |
| Baseline revision | `51693ff135be8c4149c15117980290aaad6d90da` (v0.5.2 merge) |
| Current HEAD | `51693ff135be8c4149c15117980290aaad6d90da` plus uncommitted M06 work |
| Current worktree fingerprint | `b189feed0439d3a42a3639ab801998c7a4f47a9b11dd3eecf0f87fe41c9076da` |
| Fixture SHA-256 | `bf09a9af4af64a4452013cf8437994f6d889c99653809ca4bc9dd50a130615ee` |
| Scripted proposal bytes SHA-256 | `0190fe4302f47174bb7e27bb9e986a9a2eadbbce2744d9cc6e7f02e48cce3694` (identical on both sides) |

## Fairness controls

- The baseline has no facts/evidence API, so **both** sides build the old Record
  and the 80 unrelated Records through the legacy `Remember` plus a bounded ADD
  proposal. The comparison is therefore not an ingestion-advantage test.
- The scripted deterministic proposals are byte-identical on both sides; the
  same profile (`profile-facts-compare` v1, `max_context_records = 8`, 128 KiB
  input, 16 KiB output) and the same fixture are used.
- The generated test uses a deterministic whole-second monotonic clock so job
  scheduling and Record ordering are reproducible and identical on both sides.
- The inspected probe lease is released with a deterministic `IGNORE`, so a
  later claim cannot reclaim it.
- The current-only trusted-source subject/fact-key match depends on the
  facts/evidence API; it is reported as supplemental and marked
  `not_applicable` for the baseline.

## Structural results

| Arm | Side | Result |
| --- | --- | --- |
| `B1-prior-steward-context` | exact v0.5.2 | relevant old coffee Record **not included** (rank 0 of 8) |
| `C2-relevant-steward-context` | current | relevant old coffee Record **included at rank 1** of 8 |
| `C2-trusted-source-supplemental` | current only | subject/fact-key matched Record **included at rank 1**, 1 trusted source exposed |
| `C2-trusted-source-supplemental-baseline` | baseline | `not_applicable` (no facts/evidence API) |
| `B0-exact-v0.5.2-evidence-recall` | exact v0.5.2 | retrieval fragments only, `answers_preference = false` |
| `C2-current-evidence-recall` | current | identical fragments, retrieval only |
| `GA-real-quality` | none | blocked: needs a real model plus human-reviewed data |

Verdict recorded by the runner: `b1_relevant_included = false`,
`c2_relevant_included = true`, `context_selection_changed = true`,
`context_records_identical = false`, `structural_only = true`,
`answers_preference = false`, `real_quality_claim = false`.

Interpretation bounds:

- The baseline context is the eight most recently updated active heads, so with
  80 newer unrelated Records the relevant older Record is not offered.
- The current context finds the relevant older Record first, and the
  trusted-source supplemental additionally surfaces it when a host subject and
  fact key are present.
- This is **selection** evidence. It does not show that a model would use the
  Record well, and the trusted-source arm is not a paraphrase or extraction
  result.

## Evidence Recall and the false-adoption risk

Both sides returned the same fragments for the same queries:

| Query | Expected fragment | Rank | Secondary fragment | Rank |
| --- | --- | --- | --- | --- |
| `coffee` | `I prefer coffee.` | 2 | – | – |
| `tea coffee` | `I now prefer tea over coffee.` | 1 | `Prefers coffee.` | 2 |

The `coffee` query also ranked the older derived Record `Prefers coffee.` first
and the newer tea update third. A consumer that treats a retrieved fragment as
the current preference would adopt the superseded preference. The harness
therefore reports retrieval only and sets `answers_preference = false` on both
recall arms. No conclusion about answer or consumer quality is drawn.

## Pre-existing scheduling defect (documented, not repaired)

While running this comparison the harness intermittently observed
`ClaimStewardJob` returning `found = false` with a freshly enqueued pending job
present. The cause is a **pre-existing** timestamp comparison, not this harness:

- `steward_profile.go` (`enqueueStewardJob`) writes `formatTime(now)` into
  `steward_jobs.available_at`;
- `store.go` (`formatTime`) is `value.UTC().Format(time.RFC3339Nano)`, which
  drops trailing zeros in the fractional part;
- `steward_jobs.go` compares that stored text lexicographically against a
  formatted `now` (`available_at <= ?`, `lease_expires_at <= ?`).

RFC3339Nano text is not fixed-width, so lexicographic order diverges from time
order when one value has fewer fractional digits and is a textual prefix of the
other:

```
available_at = 2026-09-15T11:23:50.12Z   (50.120)
now          = 2026-09-15T11:23:50.123Z  (50.123)
truth: available_at <= now
strings: "…50.12Z" > "…50.123Z"          because 'Z' (0x5A) > '.' (0x2E)
```

The same lexicographic pattern exists at the baseline revision
(`internal/appliance/steward_jobs.go` there), so this predates M06. The blast
radius found by inspection is confined to Steward job scheduling; capability and
Grant expiry parse timestamps and compare `time.Time`. The defect is **not
repaired here**: it is outside this comparison's scope and this document makes
no claim that fractional-time ordering is handled by the current tree.

Minimal deterministic reproduction (current tree; target for a later M04
scheduling fix, not committed here):

```go
func TestClaimFreshJobWithMixedPrecisionClock(t *testing.T) {
	var enqueued atomic.Bool
	clock := func() time.Time {
		if enqueued.Load() {
			return time.Date(2026, 9, 15, 11, 23, 50, 123_000_000, time.UTC)
		}
		return time.Date(2026, 9, 15, 11, 23, 50, 120_000_000, time.UTC)
	}
	store, auth := newGoldenStore(t, t.TempDir(), clock)
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1) // bound before Remember so a job is enqueued
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "mixed precision job", IdempotencyKey: "mixed-precision-job",
	}); err != nil {
		t.Fatal(err)
	}
	enqueued.Store(true)
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("freshly enqueued job was not claimable: available_at=...50.12Z vs now=...50.123Z")
	}
	_ = work
}
```

The comparison harness avoids this pre-existing defect by advancing a whole
second per clock call, which keeps every stored timestamp fixed-width. That is a
harness determinism choice, not a product fix.

## Raw aggregate

The following is the unmodified aggregate report from the command above.

```json
{
  "format_version": 1,
  "baseline_revision": "51693ff135be8c4149c15117980290aaad6d90da",
  "baseline_subject": "51693ff135be8c4149c15117980290aaad6d90da Merge pull request #1 from caelis-labs/codex/release/memory-backup-api-20260908",
  "current_head": "51693ff135be8c4149c15117980290aaad6d90da",
  "current_worktree_sha256": "b189feed0439d3a42a3639ab801998c7a4f47a9b11dd3eecf0f87fe41c9076da",
  "generated_at": "2026-09-15T11:32:56Z",
  "engine": "go test in temporary trees; modernc.org/sqlite real files; legacy Remember plus scripted deterministic proposals; no model, no credentials",
  "fixture": {
    "unrelated_records": 80,
    "old_receipt_text": "I prefer coffee.",
    "old_record_kind": "preference",
    "old_record_text": "Prefers coffee.",
    "unrelated_kind": "note",
    "unrelated_text": "Unrelated operating note %03d about schedules and tooling.",
    "probe_receipt_text": "I now prefer tea over coffee.",
    "profile": {
      "profile_id": "profile-facts-compare",
      "version": 1,
      "system_prompt": "organize evidence",
      "max_context_records": 8,
      "max_input_bytes": 131072,
      "max_output_bytes": 16384
    },
    "recall_queries": [
      {
        "query": "coffee",
        "expected_text": "I prefer coffee."
      },
      {
        "query": "tea coffee",
        "expected_text": "I now prefer tea over coffee.",
        "secondary_text": "I prefer coffee."
      }
    ],
    "supplemental": {
      "subject": "subject-compare",
      "fact_key": "preference.drink",
      "fact_text": "Prefers espresso.",
      "probe_receipt_text": "Espresso preference update for the compare subject.",
      "unrelated_records": 80,
      "unrelated_template": "Supplemental unrelated note %03d about schedules and tooling."
    }
  },
  "fixture_sha256": "bf09a9af4af64a4452013cf8437994f6d889c99653809ca4bc9dd50a130615ee",
  "baseline_side": {
    "side": "baseline",
    "profile_id": "profile-facts-compare",
    "profile_version": 1,
    "max_context_records": 8,
    "old_record_included": false,
    "old_record_rank": 0,
    "records_count": 8,
    "records_sha256": "65fd7f0b9cf34185d58c44bf3312ad32aecfdd93005e71464d8d8adb2480ce1f",
    "old_record_id_hash": "430f71de80eb20b0453313d3e57da393eb4c70f8168f7e230208f46367badb92",
    "receipt_attribution": {
      "baseline_fields_only": true,
      "fact_key": "",
      "side_note": "v0.5.2 ReceiptInput has no host sources, subject or fact key",
      "sources_count": 0,
      "status": "measured",
      "subject": ""
    },
    "scripted_outputs_sha256": "0190fe4302f47174bb7e27bb9e986a9a2eadbbce2744d9cc6e7f02e48cce3694",
    "recall": [
      {
        "query": "coffee",
        "expected_present": true,
        "expected_rank": 2,
        "secondary_present": false,
        "secondary_rank": 0,
        "fragments": [
          "Prefers coffee.",
          "I prefer coffee.",
          "I now prefer tea over coffee."
        ],
        "retrieval_only": true,
        "answers_preference": false
      },
      {
        "query": "tea coffee",
        "expected_present": true,
        "expected_rank": 1,
        "secondary_present": true,
        "secondary_rank": 3,
        "fragments": [
          "I now prefer tea over coffee.",
          "Prefers coffee.",
          "I prefer coffee."
        ],
        "retrieval_only": true,
        "answers_preference": false
      }
    ],
    "supplemental": {
      "reason": "the exact v0.5.2 baseline has no facts/evidence API or trusted host source attribution",
      "status": "not_applicable"
    }
  },
  "current_side": {
    "side": "current",
    "profile_id": "profile-facts-compare",
    "profile_version": 1,
    "max_context_records": 8,
    "old_record_included": true,
    "old_record_rank": 1,
    "records_count": 8,
    "records_sha256": "a11321e3c9cd35c12c113bf11560885ea64b3b66fa5513e752737eb94bd4bdf8",
    "old_record_id_hash": "958b5f09f4a4685ac5c9ebc331f4b3cb33a0dc7f09c19223e94fc6c2cbcb116c",
    "receipt_attribution": {
      "fact_key": "",
      "sources_count": 0,
      "status": "measured",
      "subject": ""
    },
    "scripted_outputs_sha256": "0190fe4302f47174bb7e27bb9e986a9a2eadbbce2744d9cc6e7f02e48cce3694",
    "recall": [
      {
        "query": "coffee",
        "expected_present": true,
        "expected_rank": 2,
        "secondary_present": false,
        "secondary_rank": 0,
        "fragments": [
          "Prefers coffee.",
          "I prefer coffee.",
          "I now prefer tea over coffee."
        ],
        "retrieval_only": true,
        "answers_preference": false
      },
      {
        "query": "tea coffee",
        "expected_present": true,
        "expected_rank": 1,
        "secondary_present": true,
        "secondary_rank": 3,
        "fragments": [
          "I now prefer tea over coffee.",
          "Prefers coffee.",
          "I prefer coffee."
        ],
        "retrieval_only": true,
        "answers_preference": false
      }
    ],
    "supplemental": {
      "evidence": "trusted source subject/fact-key match is current-only",
      "probe_receipt_matched": true,
      "receipt_fact_key": "preference.drink",
      "receipt_sources_count": 1,
      "receipt_subject": "subject-compare",
      "records_count": 8,
      "relevant_included": true,
      "relevant_rank": 1,
      "status": "measured"
    }
  },
  "arms": [
    {
      "id": "B1-prior-steward-context",
      "side": "baseline",
      "status": "measured",
      "detail": "exact v0.5.2 Claim Work.Records context after the old coffee Record, 80 newer unrelated Records and the related probe",
      "relevant_included": false,
      "records_count": 8,
      "retrieval_only": false,
      "answers_preference": false
    },
    {
      "id": "B0-exact-v0.5.2-evidence-recall",
      "side": "baseline",
      "status": "measured",
      "detail": "retrieval fragments only; a returned fragment does not mean the preference was answered or adopted",
      "retrieval_hit_rate": 1,
      "retrieval_only": true,
      "answers_preference": false
    },
    {
      "id": "C2-relevant-steward-context",
      "side": "current",
      "status": "measured",
      "detail": "current Claim Work.Records context under the same fixture, profile and scripted proposal bytes",
      "relevant_included": true,
      "relevant_rank": 1,
      "records_count": 8,
      "retrieval_only": false,
      "answers_preference": false
    },
    {
      "id": "C2-current-evidence-recall",
      "side": "current",
      "status": "measured",
      "detail": "retrieval fragments only; a returned fragment does not mean the preference was answered or adopted",
      "retrieval_hit_rate": 1,
      "retrieval_only": true,
      "answers_preference": false
    },
    {
      "id": "C2-trusted-source-supplemental",
      "side": "current",
      "status": "measured",
      "detail": "current-only trusted host source subject/fact-key match; the baseline cannot run it",
      "relevant_included": true,
      "relevant_rank": 1,
      "retrieval_only": false,
      "answers_preference": false
    },
    {
      "id": "C2-trusted-source-supplemental-baseline",
      "side": "baseline",
      "status": "not_applicable",
      "reason": "the exact v0.5.2 baseline has no facts/evidence API or trusted host source attribution",
      "retrieval_only": false,
      "answers_preference": false
    },
    {
      "id": "GA-real-quality",
      "side": "none",
      "status": "blocked",
      "reason": "extraction, model and consumer-answer quality require a real model and human-reviewed data; this comparison is structural only",
      "retrieval_only": false,
      "answers_preference": false
    }
  ],
  "verdict": {
    "b1_relevant_included": false,
    "c2_relevant_included": true,
    "context_selection_changed": true,
    "context_records_identical": false,
    "structural_only": true,
    "answers_preference": false,
    "real_quality_claim": false
  },
  "commands": [
    "git -C /Users/xueyongzhi/WorkDir/caelis-labs/memory archive --format=tar 51693ff135be8c4149c15117980290aaad6d90da | tar -x -C \u003ctmp\u003e/baseline",
    "GOWORK=off FACTS_COMPARE_FIXTURE=\u003cfixture\u003e FACTS_COMPARE_RESULT=\u003cresult\u003e FACTS_COMPARE_SIDE=baseline go test -count=1 -run=^TestFactsCompareStruct$ ./internal/appliance  (in the archived baseline tree)",
    "GOWORK=off FACTS_COMPARE_FIXTURE=\u003cfixture\u003e FACTS_COMPARE_RESULT=\u003cresult\u003e FACTS_COMPARE_SIDE=current go test -count=1 -run=^TestFactsCompareStruct$ ./internal/appliance  (in the copied current tree)",
    "go run ./scripts/facts_compare -baseline-revision 51693ff135be8c4149c15117980290aaad6d90da -unrelated 80 -output \u003creport.json\u003e"
  ],
  "notes": [
    "This is a structural, model-free comparison. It reports retrieval fragments and context selection only.",
    "A returned fragment or a selected context Record is never evidence that the user's preference was answered, adopted, extracted or paraphrased.",
    "Both sides use the same legacy Remember path and the same scripted proposal bytes, so the comparison is not an ingestion-advantage test.",
    "The current-only trusted-source subject/fact-key match depends on the facts/evidence API, which the baseline does not have; it is supplemental.",
    "Real extraction, model and consumer-answer quality comparisons remain GA-blocked."
  ]
}
```

## Correction: superseded B0 label

The raw aggregate recorded above is the historical output of the provisional run
and is **not** modified. Its baseline-side Recall arm was labeled
`B0-exact-v0.5.2-evidence-recall`, but that Recall ran *after* Steward had
organized the old coffee Record, so its fragments include the derived
`Prefers coffee.` Record. That makes it a **B1** (Steward-organized)
observation, not B0.

B0 must be receipt-only, with no Steward profile binding, no Steward job and no
Record. The corrected report is
[`docs/evidence/memory-v0.6-comparison-final.json`](memory-v0.6-comparison-final.json),
produced by the same runner with the corrected arms:

- `B0-exact-v0.5.2-no-steward-recall` — exact v0.5.2, separate fresh Store, no
  binding, asserted `jobs = 0` and `records = 0`, receipt-only fragments.
- `B1-exact-v0.5.2-steward-recall` and `B1-prior-steward-context` — the
  Steward-organized baseline Recall and context.
- `L0-current-no-steward-recall` — in-tree receipt-only control.
- `C2-relevant-steward-context`, `C2-current-steward-recall`,
  `C2-trusted-source-supplemental` — current tree.

The corrected report also records the scheduler encoding per side: the exact
v0.5.2 baseline stores `rfc3339nano_lexicographic` schedule text and the final
v0.6 candidate stores `fixed_width_utc_nanoseconds`, measured from each tree
rather than assumed.
