# Memory v0.5.0 GA Soak Evidence

Date: 2026-09-03

Release target: annotated tag `v0.5.0`. The tag and GitHub Release are created
only after the final candidate commit passes the remote quality workflow at the
same SHA.

## Command

```sh
make ga-soak
```

The command completed successfully on Darwin arm64 with Go 1.25.8 and produced
`memory.ga-soak.v1` aggregate report data. The raw generated report remains an
ignored local artifact because release evidence contains counts and timings,
not source text or a retained private database.

## Result

| Check | Result |
| --- | ---: |
| Spaces | 100 |
| Receipts | 100,000 |
| Semantic Records | 10,000 |
| Receipt status reads before restore | 100,000 |
| Receipt status reads after restore | 100,000 |
| Recall samples before / after restore | 200 / 200 |
| Provenance checks before / after restore | 100 / 100 |
| Private leak checks before / after restore | 100 / 100 |
| Private leaks before / after restore | 0 / 0 |
| Pending Steward Jobs | 0 |
| Projection and semantic index health | healthy before and after restore |

Selected wall-clock observations in milliseconds:

| Phase | ms |
| --- | ---: |
| Remember baseline receipts | 38,733 |
| Remember semantic receipts | 6,052 |
| Organize Records | 191,709 |
| Restart | 12 |
| Restart data-plane validation | 6,901 |
| Backup | 1,765 |
| Restore and rebuild | 16,931 |
| Reindex | 2,873 |

The source database was 157,884,416 bytes plus a 27,649,352-byte WAL. The
restored database was 151,613,440 bytes plus a 30,512,752-byte WAL. These are
candidate observations rather than public performance guarantees.

## Interpretation

The soak closes `GA-008` for scale, restart, backup/restore, provenance, and
private isolation at the release target. It does not validate the post-GA
Corpus protocol or any optional projection design.
