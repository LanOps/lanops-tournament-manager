# Thournaments — Backlog

Product-level TODOs that don't belong in a single PR. Grouped by theme; each
item includes enough context that future-you (or a contributor) can pick it up
cold.

## Tournament formats

Currently supports: **single elimination**, **double elimination**. Benchmark
sites — [Challonge](https://kb.challonge.com/en/article/learn-about-challonge-competition-formats-1f8j1cf/),
[start.gg](https://help.start.gg/article/bracket-setup),
[Battlefy](https://help.battlefy.com/en/articles/9396943-bracket-settings-and-ordering) —
all offer several more. Adding them unlocks non-knockout formats and multi-stage
events.

- [ ] **Round Robin** — every participant plays every other N times (1x / 2x / 3x).
  Standings by win/loss record (and tiebreakers: head-to-head, game diff, then
  seed). Good for group stages and small leagues. Schema-wise this is a new
  bracket_format + a standings view; no tree wiring needed.
- [ ] **Swiss** — fixed number of rounds (≈ log2(N) + 1). Each round pairs
  players with similar records, never repeating a matchup. First round pairs
  top half vs bottom half of the seeding. Needs a pairing engine that runs
  between rounds; otherwise stateless.
- [ ] **Group stage → knockout** (two-stage) — N groups play round-robin, top K
  from each group advance to a single/double elim bracket. The meta-event
  composition is the new piece; the sub-brackets reuse existing formats.
- [ ] **Free-for-all / multi-player matches** — matches with >2 participants,
  single result per match (1st, 2nd, ...). Schema currently hard-codes
  participant_a_id + participant_b_id; would need match_participants join table
  with placement ranks.
- [ ] **Best-of-N series** — a single match row can already carry score_a /
  score_b; a series would be modelled as multiple match rows with a parent
  match_id. Optional for now.

Ordering: Round Robin → Swiss → Two-stage → FFA. Round Robin is the cheapest
win (group-stage events are common, model is simple) and unblocks Two-stage.

## Discord bot

`internal/bot/` already has slash commands for tournament list/join/leave and
admin commands. Expand into a proper channel-aware bot:

- [ ] **Auto-announce bracket events** — post to the configured guild channel
  when a tournament enters `active`, when a match is ready, when a round
  completes, when the tournament completes. notifications.go already has the
  scaffolding (NotifyBracketGenerated etc. — three helpers) but they're not
  wired to the lifecycle.
- [ ] **Rich embed match cards** — the ready/in-progress embed includes player
  names + avatars + current score; updates in place via `s.ChannelMessageEdit`
  when the result lands. Ties into the SSE broadcast path we already have.
- [ ] **Result submission from Discord** — `/result <match_id> <winner> [scoreA]
  [scoreB]`. The web endpoint enforces participant/captain auth; reuse that.
  Helper already partly exists in `commands.go`.
- [ ] **Tournament creation wizard via DM** — for admins: `/tournament-create`
  opens a DM flow (format → max participants → description), posts the
  registration announcement back in the guild channel when done.
- [ ] **Admin role sync** — the web app already checks Discord role for admin.
  The bot should react to Discord role changes (`GUILD_MEMBER_UPDATE` event) by
  invalidating the admin cache entry immediately instead of waiting the
  5-minute TTL.

Ordering: auto-announce (lowest effort, highest user value) → rich embeds →
DM result submission → creation wizard → role sync.

## Project / Infrastructure

- [ ] **Rename project to `lanops-tournament-manager`** — update Go module path
  (`github.com/th0rn0/thournament` → `github.com/th0rn0/lanops-tournament-manager`),
  rename the GitHub repo, update Drone CI config, update any Docker image names,
  and do a global find-replace on import paths. Should be a single mechanical PR.

## Shipped (this branch, PR #2)

- [x] Dev login gated by DEV_LOGIN, fake-player seeder, LB progression fix,
  score modal + edit-past-matches, PWA shell, LanOps branding.
