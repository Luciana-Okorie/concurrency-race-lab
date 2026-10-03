# ConcurrencyRaceLab

**Day 15 — proving (not just reasoning about) the liquidity race condition
and its fix.**

This is a standalone repo, separate from OrbisFlow's codebase, whose only
job is to reproduce the classic "two concurrent transactions overspend the
same balance" bug and then prove that the fix OrbisFlow already uses
(a single conditional `UPDATE ... WHERE available >= $1`) actually holds
under real concurrent load — not just in theory.

## The experiment

An account starts with a fixed balance. N goroutines fire concurrent debits
against it at the same time. Two modes:

- **`naive`** — read the balance, decide in application code whether
  there's enough, sleep briefly (to force the race window open), then
  write the new balance. This is the anti-pattern: two goroutines can both
  read "sufficient balance" before either writes.
- **`safe`** — a single atomic `UPDATE accounts SET available = available -
  $1 WHERE available >= $1 RETURNING available`. The check and the write
  happen as one statement, so Postgres's row lock prevents the same race.

After the run, the tool reports: how many debits should have succeeded
given the starting balance, how many the database says actually succeeded,
the final balance, and whether the final balance is negative (proof of
overspend) or exactly matches the expected value (proof of correctness).

## Running it

```bash
cp .env.example .env
docker compose up -d
docker cp migrations/0001_init.sql racelab-postgres:/tmp/0001_init.sql
docker exec -it racelab-postgres psql -U racelab -d racelab -f /tmp/0001_init.sql
go mod tidy
```

Reproduce the race (expect a negative or over-drawn balance):
```bash
go run ./cmd/racetest -mode=naive -workers=50 -amount=30 -reset
```

Prove the fix (expect the balance to land exactly at zero or at the
correct remainder, never negative, and the success count to exactly match
what the starting balance can afford):
```bash
go run ./cmd/racetest -mode=safe -workers=50 -amount=30 -reset
```

With a starting balance of 1000 and amount=30, at most 33 debits can
legitimately succeed (990 debited, 10 left over). Run `naive` a few times —
it will occasionally let more than 33 through, and the final balance can go
negative. Run `safe` any number of times — it will never let through more
than 33, and the final balance is never negative.

## Ports

Deliberately isolated from every other running challenge project (see the
port registry): Postgres on host port **5448** (existing projects hold
5432–5436, 5445, 5446, 5447, 5544).

## How this connects back to OrbisFlow

OrbisFlow's `POST /v1/transactions` already uses the `safe` pattern proven
here (see `docs/day14-notes.md` in that repo). This project exists to
demonstrate — with a repeatable, adjustable, concurrent test — *why* that
pattern was chosen, by first showing what goes wrong without it.
