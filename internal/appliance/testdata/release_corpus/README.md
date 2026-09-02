# Release corpus v1

These fixtures are the public, reproducible multilingual retrieval baseline.
They contain authored, de-identified product-shaped facts rather than copied
private Session or Memory text. The subjects are fictional project codenames;
the facts contain no people, credentials, private paths, URLs, or customer
identifiers.

`manifest.json` freezes each source digest, expected expansion size, retrieval
threshold, and human-perceived latency budget. A fixture change therefore
requires an explicit privacy and quality review plus a manifest digest update.

The release gate expands the fixed templates, writes all facts through the real
durable Remember path in four batches, restarts the SQLite-backed runtime after
every batch, and then evaluates every query. It also places adversarial records
in the same Space under another exact LabelSet and in another Space under the
same LabelSet. Recall, receipt status, and consistency cursors must not cross
either boundary.

The `other` cohort includes Spanish, French, German, Japanese, Korean, and
Arabic. These fixtures measure deterministic lexical retrieval and persistence;
they do not claim general semantic understanding or model quality.
