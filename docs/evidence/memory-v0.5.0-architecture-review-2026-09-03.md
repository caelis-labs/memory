# Memory v0.5.0 Architecture Review Resolution

Date: 2026-09-03

Scope: resolution record for the external ChatGPT Pro conversation titled
“架构评审报告”, reviewed against repository revision `c25cca8` and the proposed
`v0.5.0` GA boundary. This document records decisions, not independent
experimental evidence for a future Corpus or projection implementation.

## Review conclusion

The report concludes that the implemented flat model has independent product
value and need not be replaced by a hierarchy before GA. Its accurate position
is a governed evidence and semantic-memory kernel rather than a complete
cognitive-memory system. A future Corpus ledger should be distinct from both
the Fact Memory ledger and every rebuildable retrieval projection.

The resulting authority model is:

```text
Fact Memory ledger
  Receipt -> semantic Record / Revision

Corpus Memory ledger
  Leaf -> immutable LeafRevision -> ordered Items

Optional projection substrate
  direct Item index, rollup manifests, summary/index artifacts,
  dense indexes, or graphs
```

## Finding resolution

| Review finding | Disposition | Repository resolution |
| --- | --- | --- |
| Flat Memory should not be marketed as complete long-term cognitive memory | accepted | [README](../../README.md) and the [roadmap](../memory-appliance-roadmap.md) now use the governed evidence and semantic-memory-kernel boundary |
| Fact and Corpus memory should remain independently useful public models | accepted | The roadmap defines separate Fact and Corpus ledgers that share governance substrate without reinterpreting their evidence |
| A hierarchy must be a projection, not the fact authority | accepted | LeafRevision/Item remain the Corpus evidence base; every hierarchy artifact is disposable and attributable |
| One Parent object conflates structure, summary, index, and publication | accepted | The design separates RollupManifest, SummaryArtifactRevision, IndexArtifactRevision, and ProjectionSnapshot |
| Fan-out 16, stable slots, one root, and root-first traversal were frozen too early | accepted | These are now independently versioned experimental policies, not wire or compatibility commitments |
| A direct Item index is needed before hierarchy evaluation | accepted | P3 delivers direct QueryCorpus first; P4 adds optional projections |
| Query evaluation should isolate direct, collapsed cross-level, controlled-expansion, and summary-assisted gains | accepted | The [evaluation plan](../memory-appliance-evaluation.md) defines independent controls and a blind holdout |
| Tree implementation should not block `v0.5.0` | accepted | Corpus and projection work remains post-GA and independently disableable; Fact Memory Recall is unaffected |
| Future public operations need separate Corpus, materializer, and management boundaries | accepted as a P3 design inventory | Working namespaces and operations are documented for prototyping, but none is added to or frozen by the v0.5 API |
| Academic and community systems suggest collapsed retrieval, modular memory kinds, and optional graph projections | accepted as research direction | These claims do not establish product defaults; local conformance, privacy, rebuild, and comparative evaluation remain mandatory |

## GA corrections

The review also exposed two concrete pre-GA closure items unrelated to the
future projection design:

- remove the deprecated direct-proposal `Generator` SDK callback so
  `ModelGenerator` is the only provider integration surface;
- publish `memory-v0.5.0` as the first schema compatibility floor and prove the
  byte-identical final prerelease baseline promotes without losing accepted
  evidence.

These items are implementation and release gates. No Corpus table, tree
algorithm, new model call, or new public Corpus operation is required for
`v0.5.0`.

## Accepted residual scope

P3 must prototype and validate the exact Corpus operation set, consistency
contract, lifecycle vocabulary, and additive schema before freezing an API.
P4 must compare topology choices and atomic projection publication. P5 must
show material end-to-end benefit over direct Item retrieval. Dense and graph
projections remain optional hypotheses, not roadmap commitments.
