# Index: targetconfig-stale-not-ready-after-transient-flap

Tracks status for the implementation tickets under this feature. See `../spec.md` for the full spec.

**Convention:** when a ticket is implemented, its code changes land as one commit, separate from every other ticket's commit — do not batch multiple tickets into one commit.

| # | Title | Status | Blocked by |
|---|-------|--------|------------|
| [01](01-skip-wake-on-unchanged-desired-hash.md) | Skip `wake()` on unchanged desired hash in `TargetRuntime.SetDesired` | done | None |
| [02](02-requeue-delay-investigation-logging.md) | Requeue-delay investigation logging (best-effort, non-blocking) | done | None |
| [03](03-self-heal-targetforconfig-on-noop-reconcile.md) | Self-heal `TargetForConfig` condition on no-op reconcile | done | None |

## Status values

- `blocked` — one or more blockers not yet `done`
- `ready-for-agent` — unblocked, not yet started
- `in-progress` — claimed, being worked
- `done` — implemented, tested, and committed

Update this table whenever a ticket's `Status:` line changes.
