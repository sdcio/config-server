# 04 — Narrow `saveSnapshot` to backstop-only; delete the `buildIntentInputs` orphan-key loop

**Spec:** `.scratch/last-applied-snapshot-write-at-apply/spec.md` (see "Rollback — remove wrongful paired-PR assumptions" and "`saveSnapshot` (backstop)")

**What to build:** Post-`TransactionConfirm` `saveSnapshot` (`pkg/reconcilers/targetconfig/reconciler.go`) stops full-replacing `Spec.Configs` and instead only prunes entries with no matching `SensitiveConfig` and refreshes incidental metadata (hashes, `LastKnownGoodSchema`) — via the same per-key merge-patch mechanism as ticket 02's apply-time write, so it can never clobber what an apply-time write or a rollback just wrote. It must continue to never run on a failed transact (existing `HasErrors` early return stays). `buildIntentInputs`'s orphan-key loop is deleted outright, not simplified — it's now redundant with apply-time delete + the backstop prune, and its own comment ("re-send the delete") never matched its actual `hasChange`-only behavior.

**Blocked by:** 02 (needs the per-key merge-patch write path to reuse)

- [ ] `saveSnapshot` no longer does get-or-create + full `Spec` replace; it issues a per-key merge-patch prune for orphaned entries only, plus metadata refresh
- [ ] `saveSnapshot` still never runs on failed transact
- [ ] `buildIntentInputs`'s orphan-key loop (the block that sets `hasChanged = true` for keys present in the snapshot but missing from `scByName`) is deleted, not modified
- [ ] `pkg/reconcilers/targetconfig/reconciler_test.go` covers: backstop prune removes an orphaned key without touching an unrelated key an apply-time write just wrote (simulated race); backstop never runs after a failed transact
- [ ] Append a retraction note to `config-server/.scratch/target-snapshot-backed-config-read/spec.md` (main checkout, branch `sonic-device-profile` — separate from this worktree) pointing at ADR 0003 (data-server), since that spec's "worst case redundant re-push" conclusion is the thing being retracted and nothing currently marks it as superseded where it lives
