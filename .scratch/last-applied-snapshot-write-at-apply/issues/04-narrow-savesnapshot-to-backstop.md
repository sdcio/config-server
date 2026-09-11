# 04 — Narrow `saveSnapshot` to backstop-only; delete the `buildIntentInputs` orphan-key loop

Status: done

**Spec:** `.scratch/last-applied-snapshot-write-at-apply/spec.md` (see "Rollback — remove wrongful paired-PR assumptions" and "`saveSnapshot` (backstop)")

**What to build:** Post-`TransactionConfirm` `saveSnapshot` (`pkg/reconcilers/targetconfig/reconciler.go`) stops full-replacing `Spec.Configs` and instead only prunes entries with no matching `SensitiveConfig` and refreshes incidental metadata (hashes, `LastKnownGoodSchema`) — via the same per-key merge-patch mechanism as ticket 02's apply-time write, so it can never clobber what an apply-time write or a rollback just wrote. It must continue to never run on a failed transact (existing `HasErrors` early return stays). `buildIntentInputs`'s orphan-key loop is deleted outright, not simplified — it's now redundant with apply-time delete + the backstop prune, and its own comment ("re-send the delete") never matched its actual `hasChange`-only behavior.

**Blocked by:** 02 (needs the per-key merge-patch write path to reuse)

- [x] `saveSnapshot` no longer does get-or-create + full `Spec` replace; it issues a per-key merge-patch prune for orphaned entries only, plus metadata refresh
- [x] `saveSnapshot` still never runs on failed transact
- [x] `buildIntentInputs`'s orphan-key loop (the block that sets `hasChanged = true` for keys present in the snapshot but missing from `scByName`) is deleted, not modified
- [x] `pkg/reconcilers/targetconfig/reconciler_test.go` covers: backstop prune removes an orphaned key without touching an unrelated key an apply-time write just wrote (simulated race); backstop never runs after a failed transact
- [x] Append a retraction note to `config-server/.scratch/target-snapshot-backed-config-read/spec.md` (main checkout, branch `sonic-device-profile` — separate from this worktree) pointing at ADR 0003 (data-server), since that spec's "worst case redundant re-push" conclusion is the thing being retracted and nothing currently marks it as superseded where it lives

## Comments

Implemented on `config-server` worktree at `/home/mava/projects/config-server-worktrees/config-server-cache-backend-race` (branch `config-server-cache-backend-race`).

- `saveSnapshot` now merge-patches only orphan `Spec.Configs` keys (`null` per RFC 7396) plus `LastKnownGoodSchema` from the datastore handle. It does not get-then-replace, does not create a missing `TargetSnapshot`, and does not copy live `SensitiveConfig` payloads onto remaining keys — those hashes/payloads stay as apply-time `Modify` wrote them, so a backstop cannot mix live-desired content into last-applied.
- `buildIntentInputs`'s orphan-key loop is gone. An orphan-only snapshot no longer forces a transact; the backstop runs after the next successful Confirm, which is the spec's recovery-only posture.
- Tests: `TestReconcile_SaveSnapshot_PruneOrphanDoesNotClobberConcurrentApplyWrite` injects a concurrent apply-time write during the snapshot Patch and asserts the ghost key is gone, the racing key keeps the apply-time hash, and `LastKnownGoodSchema` is refreshed. `TestReconcile_SaveSnapshot_SkippedOnFailedTransact` asserts a failed `TransactionSet` leaves the snapshot untouched. `TestReconcile_SaveSnapshot_RefreshesSchemaAfterApplyTimeCreate` asserts schema is still patched when loadSnapshot saw no object but apply-time created one during the transact (Patch NotFound is a no-op; a just-created object is patched).
- Retraction note landed on the main checkout's `.scratch/target-snapshot-backed-config-read/spec.md` (not this worktree), as the ticket required.
