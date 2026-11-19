# Shared rule fixtures

The video domain rules exist twice — in Go (`server/features/video`) and in Kotlin
(`androidApp`), because the Android app computes them on the device (SPEC D-040). These JSON files
are the cases **both** test suites run, so a rule changed on one side fails the other.

- `show-status.json` — `RecomputeShowStatus` (D-010, D-012): a stored status, the show's episodes
  and which are watched in; the status afterwards out (`null` = no state row).
- `next-episode.json` — the Up Next pick: the first non-special episode strictly after the highest
  watched position; `null` = the show is not listed.

- `tmdb-sync.json` — the TMDB account's two numeric rules (D-029, D-036): how a pulled rating is
  rounded (half up, clamped 1..10) and how a failed send backs off (30 s doubling to an hour).
- `ticket-box-restore.json` — the Ticket Box restore (D-023): how one of the file's instants is
  normalised or refused against `now`, which entries of a file are skipped (kind and reason, in
  order) and which movie and show groups remain, and the hint an unresolved episode gets from the
  numbers held for its season.

Every case is evaluated with `today` as the user's day. Go: `status_fixtures_test.go` in
`video/common` and `fixtures_test.go` in `video/upnext`. Kotlin: `RuleFixturesTest`. The TMDB and
Ticket Box cases: `rule_fixtures_test.go` in `video/tmdbaccount` and `ticketbox_fixtures_test.go`
in `video/restore`; Kotlin: `TmdbSyncTest` and `TicketBoxRestoreTest`.
