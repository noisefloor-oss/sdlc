# Deck integration

noise-sdlc is fully usable standalone. Integration with the `noise`
dispatcher and the Noisefloor Deck desktop app is opt-in in both directions:

- `noise-sdlc sync export` emits a deterministic, credential-free bundle of
  the public harness registry, execution templates, and interaction contract
  that Deck can consume.
- Deck answers with a runtime receipt, delivered through
  `noise-sdlc sync accept RECEIPT`, `noise-sdlc sync pull URL` (loopback
  only), or the web UI. The receipt is observation-only; this tool recomputes
  all routing conclusions from its own catalog. `sync status` reports
  `in-sync` or `catalog-drift` from the recorded digests.
- `noise-sdlc harness candidates` turns harnesses Deck has seen but this
  catalog does not know into a curation queue. It is not a support claim.

Disconnecting or never connecting leaves every standalone feature intact.
The exchange formats are the export and receipt schemas listed in
[Compatibility](compatibility.md).

