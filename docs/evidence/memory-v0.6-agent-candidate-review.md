# Memory v0.6 candidate trajectory review by an independent AI agent

**Status: completed under the user-authorized AI substitution.** The separate `/root/trajectory_audit` agent reviewed all **208 concrete trajectories** (26 templates × 8 subjects) and **448 controlled-topic query expectations**. Decisions: **208 accept, 0 reject, 0 needs revision**, limited to the explicit structured lifecycle and fixed vocabulary contract. `human_reviewed_cases = 0`; this is AI review, not human annotation.

The user explicitly authorized the assistant or an independent subagent to perform this review. This completes that substituted review task. It does not by itself qualify production model extraction, arbitrary natural paraphrases, background use, or final answers, and it does not mean that GA is complete.

## Evidence and method

- [Per-case JSON decisions](memory-v0.6-agent-candidate-review.json) contain stable IDs, concrete source/subject inputs, all lifecycle operations, reviewer-authored current expectations, every query expectation, and traceable reasons.
- Input fixture: [`candidates.json`](../../internal/appliance/testdata/facts_eval/candidates.json), SHA-256 `c6c7649fefdca6bc917d5528ed7a28624413fb8360e72a649f7564ee08959715`.
- Reviewed the actual expansion in [`facts_eval_candidates_test.go`](../../internal/appliance/facts_eval_candidates_test.go), the fixed-clock/auth harness, and the versioned trusted-source and current-fact contract. Initial and follow-up source-review hashes are recorded in the JSON. The main agent subsequently repaired the two assertion gaps identified below; this reviewer re-read those changes.
- Read the literal statements and separately adjudicated all 26 expected texts and their qualifiers; checked all eight subject substitutions, unique source coordinates and all 208 expanded input/expectation pairs. A script serialized those decisions and checked enumeration. Generator labels and prior test PASS were not the basis for acceptance.
- This is a static fixture/contract review. No runtime result is claimed by this document; validation gates are separate evidence.

## Why these expectations are accepted

All initial `user_quote` assertions remain pending. A distinct `f2` source fragment supplies an explicit trusted-host `structured_confirmation`, exact pending text, target ID and revision. The 72 change trajectories then supply a verified replacement and explicit February onset. Other trajectories retain their verified January statement. At the fixed September clock each lineage has one applicable current statement. These host-supplied dates and roles are fixture inputs; the reviewer has not inferred them from prose or vouched for a real user confirmation.

Each opaque subject is identical in its source, mutation and read request. It is attribution within the authorized `space-bot-a` / `workspace:alpha` partition, and supplies no access authority. First-person wording is not parsed into another identity. Qualifiers such as morning, 尽量 and scheduling remain in the full text; no all-day habit, strict dietary restriction or residence is inferred.

Old-value queries such as `tea`, `茶`, `中文` or `vegetarian` are accepted only as fixed **topic selectors**. They can retrieve a later coffee/English/non-vegetarian statement. They are not synonyms for the new fact and must not cause a final answer to affirm the old preference.

## Findings and limits

1. **F01 — closed after source review.** The initial runner ignored the frozen label when choosing its assertion. The repaired runner now uses `candidate.ExpectCurrent` for current reads and controlled queries. The fixture and all 208 verdicts are unchanged. Initial discovery and follow-up hashes remain in the JSON.
2. **F02 — closed after source review.** The repaired runner asserts empty facts/background before confirmation; final subject/Space and exact record/revision; and the complete, duplicate-free ReceiptID → Source mapping. Confirmed unchanged heads require both quote and confirmation sources, while changed heads require the new source. Controlled-query hits check the same identity and provenance. These source changes close the identified assertion gap; runtime execution evidence is recorded separately by the main agent.
3. **Only 26 semantic templates.** Eight subject substitutions do not create 208 independent semantic examples. Every case has a fresh store and one fact lineage, so this set does not demonstrate same-store subject/Space isolation or ranking among distractors.
4. **Temporal and lifecycle gaps.** The candidates provide fixed historical onsets and current reads, with no expiry, nil onset, interval boundary, exception, correction, denial, forgetting or exact-condition cases. Other suites may cover these separately.
5. **The dictionary still biases the sample.** The 448 queries come from the implementation’s controlled vocabulary. AI review does not turn this sample into a blind holdout or remove circular selection. Neither the 0.95/0.90 production quality targets nor consumer/model behavior are qualified here.

## Template decisions

Every row below expands to the eight concrete case IDs recorded in the JSON; reasons are intentionally reused for subject-only variants.

| Template | Cases | Decision | Reviewer rationale |
| --- | ---: | --- | --- |
| `drink-coffee-en` | 8 | accept | The speaker explicitly prefers morning coffee. Preserve the morning qualifier in the text; the unconditioned host metadata does not prove a morning-only context filter. |
| `drink-tea-en-change` | 8 | accept | The later verified statement replaces tea with coffee as the drink preference. A tea query requests the controlled drink topic; it must return the current coffee statement, never assert that tea remains preferred. |
| `drink-coffee-zh` | 8 | accept | The Chinese statement explicitly says the speaker prefers coffee. 咖啡 names the beverage and 饮品 names its topic; neither needs an inferred preference value. |
| `drink-tea-zh-change` | 8 | accept | The later Chinese statement explicitly changes the preference from tea to coffee. 茶 is accepted only as a drink-topic selector, not a synonym for the new coffee preference. |
| `drink-mixed-zh-en` | 8 | accept | The mixed-language statement identifies the speaker's morning drink as coffee. Preserve the morning qualifier and do not turn it into an all-day preference or a machine-interpreted condition. |
| `drink-beverage-en-change` | 8 | accept | The explicit verified change moves the preference from sparkling water to still water. Here still water is a beverage value, not a claim that the old sparkling preference continues. |
| `drink-oolong-zh` | 8 | accept | The speaker explicitly selects oolong tea as an everyday drink. 饮料 and 饮品 are topic terms; 茶 is a category match. The exact oolong qualifier remains in the fact. |
| `diet-vegetarian-en` | 8 | accept | The statement explicitly attributes a vegetarian diet to the speaker. diet and vegetarian are appropriate controlled topic lookups; no stricter vegan restriction is inferred. |
| `diet-vegetarian-zh` | 8 | accept | The Chinese first-person statement explicitly reports vegetarian eating. Preserve the exact wording; 素食 must not be upgraded to a more specific vegan or medical restriction. |
| `diet-zh-change` | 8 | accept | The verified later statement explicitly negates the earlier vegetarian habit. The expected current text retains 不再; matching 素食 retrieves this negated current statement and does not affirm vegetarianism. |
| `diet-en-change` | 8 | accept | The verified later statement ends the vegetarian diet. The expected current text retains stopped; a vegetarian topic hit does not mean the user currently follows that diet. |
| `diet-mixed` | 8 | accept | The mixed-language first-person statement explicitly attributes vegetarian eating. Keep the full statement unchanged; the English and Chinese queries select the same diet topic. |
| `diet-light-zh` | 8 | accept | The statement describes an attempt to eat lightly. Preserve 尽量 so a tentative practice is not converted into an absolute restriction; diet and 饮食 name the subject matter. |
| `language-en` | 8 | accept | The speaker explicitly requests English replies. The language key and both query terms match that request; no language proficiency or nationality is inferred. |
| `language-zh` | 8 | accept | The speaker explicitly requests Chinese replies. Preserve this reply preference; 中文 and 语言 are topic lookups, not evidence of nationality or other identity. |
| `language-zh-change` | 8 | accept | The verified later request changes replies from Chinese to English. 中文 can select the controlled language topic, but the returned current text must remain the English request. |
| `language-en-change` | 8 | accept | The verified later statement changes the reply preference from English to Chinese. The expected current text directly states the new Chinese reply preference. |
| `language-mixed` | 8 | accept | The mixed-language statement explicitly names Chinese as the preferred language. Keep its general wording instead of inventing proficiency or a more specific use context. |
| `language-zh-only` | 8 | accept | The Chinese statement explicitly requests Chinese as the reply language. Both aliases select the language topic without altering the request. |
| `language-en-only` | 8 | accept | The request explicitly sets English as the reply language. The trusted subject attributes this request; no textual named-entity inference is needed. |
| `timezone-en` | 8 | accept | The first-person statement explicitly supplies Asia/Shanghai as its timezone. Preserve the zone identifier and do not infer physical residence or a fixed UTC offset. |
| `timezone-zh` | 8 | accept | The Chinese first-person statement explicitly supplies Asia/Shanghai. 时区 and time zone identify the timezone topic; the IANA-style identifier stays unchanged. |
| `timezone-en-change` | 8 | accept | The verified later statement explicitly changes the timezone from UTC to Asia/Tokyo. The expected current fact contains Tokyo; no old UTC preference should be adopted now. |
| `timezone-mixed` | 8 | accept | The statement sets a scheduling timezone, not necessarily the speaker's residence. Accept the host-assigned subject and preserve the scheduling wording; do not broaden it into a physical-location fact. |
| `timezone-zh-change` | 8 | accept | The verified Chinese statement explicitly changes the timezone from UTC to Asia/Tokyo. The exact new zone and change wording remain in the current fact. |
| `timezone-en-berlin` | 8 | accept | The statement sets the timezone used for scheduling. The trusted subject supplies attribution; preserve the scheduling scope and do not infer residence or a constant offset across daylight-saving changes. |

## Follow-up source verification

The repaired runner SHA-256 is `0a44b05b474b6526e4d9a84477ce8203cd704031e894690b2b35fe22c6827e14`; the revised manifest SHA-256 is `0fff2f854a1cc8c50f34030236007df750c84577b9d3b21da54e6ead28b66e58`. The manifest preserves zero human-reviewed cases and identifies real-model/holdout quality as the remaining qualification gap while acknowledging the authorized AI review. The updated owning [Facts contract](../memory-v0.6-facts.md), SHA-256 `108be97b0d9d000e4e065147810384cae5c355e9cfc2ef3626a9f983c046ee20`, was also re-read; the new barrier/condition/exception clarifications do not change these candidate decisions. This reviewer verified source changes only; subsequent runtime gates are attributed to the main agent, not inferred from this review.

## Artifact identity

The detailed JSON SHA-256 at generation is `c4d4d5f9ca5cf025e7e6f3bf2992d8cabeccf1f4eb2c26ea4f2b19fca19d1a48`. The fixture remains unchanged. This review writes only its JSON/Markdown evidence artifacts and does not change implementation, source roles, adoption authority, or corpus human-review labels.
