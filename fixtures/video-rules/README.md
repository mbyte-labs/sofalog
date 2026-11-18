# Shared rule fixtures

The video domain rules exist twice — in Go (`server/features/video`) and in Kotlin
(`androidApp`), because the Android app computes them on the device (SPEC D-040). These JSON files
are the cases **both** test suites run, so a rule changed on one side fails the other.

- `show-status.json` — `RecomputeShowStatus` (D-010, D-012): a stored status, the show's episodes
  and which are watched in; the status afterwards out (`null` = no state row).
- `next-episode.json` — the Up Next pick: the first non-special episode strictly after the highest
  watched position; `null` = the show is not listed.

Every case is evaluated with `today` as the user's day. Go: `status_fixtures_test.go` in
`video/common` and `fixtures_test.go` in `video/upnext`. Kotlin: `RuleFixturesTest`.
