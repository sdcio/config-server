# Spec: last-applied TargetSnapshot writes at apply time (config-server cache backend)

**Status:** ready-for-agent

**Paired branches (unmerged — clean up before implementing):** config-server `config-server-cache-backend` (PR #476), data-server `config-server-cache-backend` (#471), integration-tests `config-server-cache-backend` (#115).

**Predecessor:** grilling session 2026-08-26 (`d412416a`, rounds 1–3) + a follow-up grilling round on write-path mechanics (round 4, 2026-09-01) that this revision closes out. See `data-server/pkg/cache/docs/adr/0003-config-server-write-path-real-last-applied-writes.md` for the "why," this file for the "what."

## Problem Statement

Under `Cache.Type: config-server`, data-server reads **last-applied** Intent values from `TargetSnapshot` via colocated `ConfigReadService` Get/List, then `LoadAllButRunningIntents` imports every map entry into the transaction tree before applying this RPC's intents. That overlay model is correct and must stay.

The paired PRs wired reads to `TargetSnapshot` but left writes wrong: `IntentModify` / `IntentDelete` are silent no-ops (`noopIntentWriter`), and TargetConfig only updates `TargetSnapshot` after `TransactionConfirm` via `saveSnapshot`. Last-applied therefore lags behind what the datastore already applied — unlike `Cache.Type: local`, where the applied-intent store updates inside `TransactionSet` at apply time.

That lag causes real failures:

- **Ghost intents:** A deleted Intent can remain in `TargetSnapshot` while its Config/SensitiveConfig is already gone. The next `LoadAll` rehydrates dead config into the tree; an unrelated delete in the same RPC fails validation (leafref), and `ProcessErrors` can mark unrelated Configs failed as "unknown intent" (CI: 02-CRUD SROS teardown hung deleting `customer` while `intent1` ghosted the tree).
- **Apply→Confirm window:** Deviations, blame, and revert call `LoadAll` without holding the transaction lock. They see stale last-applied until post-Confirm `saveSnapshot` — fake deviations and inconsistent diffs on updates.
- **Rollback:** Timeout/cancel rollback restores device state via another `TransactionSet` but does not restore last-applied if writes are no-ops — device and last-applied diverge.
- **Recovery:** Crash after apply but before post-Confirm snapshot can replay deleted config from a stale map entry.

A prior design decision on the branch explicitly treated stale `TargetSnapshot` as harmless ("worst case redundant re-push"). That is **wrong** and must be retracted, not patched around.

## Solution

Make **last-applied membership and value** on `TargetSnapshot.Spec.Configs` update at the same moment as `Cache.Type: local` — inside `TransactionSet`, when southbound apply succeeds and generic code calls `IntentModify` / `IntentDelete` — not gated on `TransactionConfirm`.

**Membership rule:** a name is last-applied iff it is a key in `Spec.Configs`. On delete apply, **remove the key** (no tombstone labels/annotations). Rollback `IntentModify` restores the entry.

**Read path unchanged:** `ConfigReadService` Get/List continue to serve last-applied from the map; no LoadAll-side filters. (The service itself is renamed and merged with the new write RPCs — see "Write path" below; its read behavior is unchanged.)

**`saveSnapshot` demoted:** After Confirm + `ProcessSuccess`, TargetConfig may still reconcile snapshot metadata, but it must not be the authority for membership and must not blind-replace the map in a way that clobbers apply-time or rollback writes.

**Clean up the branch:** Remove or revert wrongful paired-PR artifacts (see Implementation Decisions — Rollback). Do not layer compensating logic (ProcessErrors-only fixes, LoadAll filters, tombstones, orphan-loop hacks) on top of the broken write model.

## User Stories

1. As a data-server maintainer, I want `LoadAllButRunningIntents` to load only Intents that are still last-applied on this datastore, so that deleted config is not reintroduced into the validation tree.
2. As a data-server maintainer, I want last-applied to update when `IntentModify` / `IntentDelete` run inside `TransactionSet`, so that `Cache.Type: config-server` matches `Cache.Type: local` timing.
3. As a data-server maintainer, I want deviation detection to compare running state against last-applied immediately after apply, so that ghost deviations are not reported during the Confirm window.
4. As a data-server maintainer, I want transaction rollback to restore last-applied as well as device state, so that timeout/cancel does not leave the applied-intent store ahead of the device.
5. As a config-server maintainer, I want apply-time snapshot writes on the colocated controller, so that generic datastore code does not kube-client `TargetSnapshot` directly.
6. As a config-server maintainer, I want `IntentDelete` to remove `Spec.Configs[name]` and `IntentModify` to upsert the applied encrypted spec, so that membership is a single map with no parallel deleted index.
7. As a config-server maintainer, I want post-Confirm `saveSnapshot` to prune orphan keys and refresh hashes/schema only, so that it cannot resurrect keys rollback removed or erase keys apply just wrote.
8. As an operator running cache-backend integration tests, I want ConfigSet teardown deletes to complete without leafref failures from ghost snapshot entries, so that CI reflects production correctness.
9. As an operator recovering after pod restart, I want recovery to replay only Intents still present in last-applied, so that a delete applied before crash is not pushed again.
10. As a maintainer reading `pkg/cache/CONTEXT.md`, I want **last-applied** defined at apply time (not `TransactionConfirm`), so that vocabulary matches behavior.
11. As a maintainer, I want ADR 0001's no-op write clause superseded by a new ADR, so that future readers do not reintroduce the bug.
12. As a data-server maintainer, I want `LoadAll` of intents not in this RPC to remain required behavior, so that the overlay model is not mistaken for a bug.
13. As a config-server maintainer, I want TargetConfig not to watch `TargetSnapshot`, so that apply-time snapshot patches do not enqueue redundant reconciles.
14. As a maintainer, I want optional `ProcessErrors` hardening so validation errors on LoadAll-only owners do not fail unrelated Configs, as a safety net only.
15. As a maintainer, I want unit tests for the ghost+customer-delete signature first for fast feedback, and integration tests on cache-backend for end-to-end proof before merge.
16. As a config-server maintainer, I want the local RPC surface merged into one renamed service instead of split across a read service and a new write service, so that there's one proto file, one server registration, and one client dial for what is fundamentally one capability over one resource (`TargetSnapshot`).
17. As a data-server maintainer, I want the applied payload itself (not a re-read signal) to cross the write RPC, so that config-server's record of last-applied can never silently diverge from what was actually pushed by picking up a newer, not-yet-applied desired value.
18. As a config-server maintainer, I want the `TargetSnapshot` write (both the apply-time upsert/delete and the backstop prune) to use a targeted per-key patch instead of get-then-replace, so that the two writers can't race and stomp on each other.
19. As a data-server maintainer, I want a failed last-applied delete-write to hard-fail the transaction (matching modify), so that "backstop only" for `saveSnapshot` is actually true — a write path that can silently fail leaves exactly the ghost-entry bug this spec closes, one layer down.

## Implementation Decisions

### Rollback — remove wrongful paired-PR assumptions (do not evolve on them)

- **Remove `noopIntentWriter` from the config-server `Client` composition.** ADR 0001's "unconditional silent no-ops" for `InstanceIntentModify`/`InstanceIntentDelete` is incorrect for last-applied; supersede with ADR 0003 (data-server repo). `noopIntentWriter` may remain for genuinely read-only backends only — not config-server.
- **Retract** the prior `target-snapshot-backed-config-read` spec decision that stale `TargetSnapshot` is non-correctness-breaking and "worst case redundant re-push."
- **Narrow `saveSnapshot`** from full map rebuild authority to backstop-only (prune keys whose SensitiveConfig is gone; refresh hashes / `LastKnownGoodSchema`). Revert the mental model that post-Confirm snapshot is when last-applied is defined.
- **Do not add** tombstone labels/annotations, LoadAll-side deleted filters, or make `ProcessErrors` unknown-intent handling the primary fix.
- **Delete `buildIntentInputs`'s orphan-key loop outright** (not "review/simplify" — this revision settles it): once apply-time `IntentDelete` removes the key, and the backstop prune catches any orphan left by a failed write (now loud, per the hard-fail decision below, not silent), the loop is redundant. Its own comment ("re-send the delete") doesn't even match its actual `hasChange`-only behavior — a second, mismatched compensator is worse than no compensator.

### Write path (primary) — RPC contract (settled, round 4)

- **One merged, renamed local service** — `ConfigSnapshotService` (`Get`/`List`/`Modify`/`Delete`) — replaces `ConfigReadService` in `sdc-protos`. Not a second sibling write service: this is a single colocated, single-consumer, localhost-bound API over one resource (`TargetSnapshot`); two services would only add a second proto/registration/dial for no isolation benefit. Capability segregation stays at the Go layer (data-server's `IntentReader`/`IntentWriter`), which a single generated gRPC client still satisfies narrowly via structural typing. See ADR 0003 (data-server) for the full reasoning.
- **Per-intent RPCs**, not batched — `Modify(intent)` / `Delete(name)` calls happen once per intent, matching the existing apply-loop call shape exactly. Batching per-`TransactionSet` is an explicitly deferred future optimization.
- **The applied payload crosses the wire on `Modify`** — the same encrypted `SensitiveConfigSpec`-shaped payload `saveSnapshot` already builds, built at apply time from the just-exported `protoIntent` (encrypt via the existing KeyRing pattern). `Modify` does **not** re-read config-server's own current `SensitiveConfig` to reconstruct the entry — that would silently reintroduce live-vs-last-applied conflation if the desired value has already moved on by the time the RPC lands. Incidental fields (`Revertive`, `Lifecycle`) are filled from config-server's current `SensitiveConfig`/`Config` at write time — acceptable staleness, since they only affect the *next* reconcile's push decision, not device-truth payload content.
- **`Delete` failure hard-fails the transaction**, same as `Modify` — this is a deliberate change from the pre-fix code's asymmetry (where `IntentDelete` failure was log-only). A write path that can silently fail on delete leaves exactly the ghost-entry bug this spec exists to close, just relocated into the write RPC's own failure handling.
- **Concurrency: server-side JSON merge-patch on the single map key**, not Get→mutate→Update, for both the apply-time `Modify`/`Delete` write and the backstop `saveSnapshot` prune. A single-key patch means the apply-time writer and the backstop prune touching different (or even the same) keys around the same time can't lose each other's update the way a full-object replace could.
- data-server `ConfigServerCache` (or a small dedicated client) implements `IntentWriter` by calling `ConfigSnapshotService.Modify`/`Delete` — composed into the full `Client` at `createConfigServerCacheClient`, replacing `noopIntentWriter` for this backend.
- Generic `lowlevelTransactionSet` apply loop unchanged in timing — same calls as local backend.

### Read path (unchanged)

- `ConfigSnapshotService.Get`/`List` (the renamed, merged surface): range `TargetSnapshot.Spec.Configs`; missing key → NotFound / omitted from List. Identical behavior to today's `ConfigReadService`, just relocated onto the merged service.

### `saveSnapshot` (backstop)

- May run after successful Confirm + `ProcessSuccess`.
- May prune entries with no matching SensitiveConfig; may refresh metadata — via the same per-key patch mechanism as the apply-time write (see Concurrency above), not a full `Spec.Configs` replace.
- Must not full-replace `Spec.Configs` from reconcile-start state in a way that overwrites apply-time or rollback writes.
- Must not run on failed transact (existing `HasErrors` early return stays).

### Vocabulary

- **Last-applied:** value last successfully applied to device; updated at `IntentModify`/`IntentDelete` inside `TransactionSet`; not gated on Confirm. Update `data-server/pkg/cache/CONTEXT.md` accordingly (membership = map key present; no tombstones) — done as part of ADR 0003.
- Avoid "expected state" and "confirmed" for this concept (`TransactionConfirm` is different).

### Cross-repo coordination

- Implement on unmerged paired branches; land config-server write API + data-server client together.
- `sdc-protos`: rename `config_read.proto`'s `ConfigReadService` → `ConfigSnapshotService`, add `Modify`/`Delete` RPCs alongside the existing `Get`/`List`. Single consumer (`ConfigServerCache` in data-server, both `GRPCConfigReader`-equivalent read and new write calls) — no external break, per the same reasoning the `target-snapshot-backed-config-read` spec already used to justify unconditional wire-shape changes to this service.
- Record ADR 0003 in data-server `pkg/cache/docs/adr/` — done; note supersession of ADR 0001's write clause.

## Testing Decisions

**Principle:** Two levels, both required. **Unit tests first** — fast red/green while developing. **Integration tests second** — end-to-end proof on the cache-backend stack before merge. Unit tests are not a substitute for integration tests; integration tests are not a substitute for unit tests.

### Level 1 — unit tests (first, required)

Drive the existing `TransactionSet` apply path via `cache.Client` / `CacheClientBound` with a mock or fake `ConfigSnapshotService`. Assert:

- After delete apply + `IntentDelete`, next load does not return that Intent (ghost signature).
- After modify apply + `IntentModify`, next read returns the new value.
- After delete then rollback `TransactionSet` of old content, map key is restored.
- `saveSnapshot` backstop cannot clobber a key removed at apply time.
- A failed `Delete` RPC hard-fails the transaction (regression test for the asymmetry fix).
- Concurrent apply-time write and backstop prune (simulated) via the merge-patch mechanism don't lose either update.

**Prior art:**

- data-server: `pkg/datastore/transaction_rpc_test.go` (gomock `CacheClientBound`, IntentModify expectations).
- data-server: `pkg/server/cache_test.go` (config-server client composition).
- config-server: `pkg/sdc/configread/handlers_test.go` (TargetSnapshot fixtures); write-RPC handler tests (new).
- config-server: `pkg/reconcilers/targetconfig/reconciler_test.go` (fake client reconciler tests).

Optional narrow unit: `ProcessErrors` does not fail unrelated Config when validation error owner was LoadAll-only.

### Level 2 — integration tests (required before merge)

Robot suites on `integration-tests` branch `config-server-cache-backend` with `Cache.Type: config-server` deployed. Prior art: `tests/05-cache-backend/10-srl-cache-backend.robot`.

**Required scenarios:**

1. **Ghost + unrelated delete (CI regression):** Multi-intent target (e.g. `intent1` + `customer` on SROS). Delete `intent1`, confirm gone from device and TargetSnapshot. Delete `customer` separately — must succeed; ConfigSet teardown completes (no leafref ghost, no "unknown intent").
2. **Apply→Confirm window:** After delete apply, deviation/blame/GetIntent must not treat deleted intent as last-applied before post-Confirm `saveSnapshot` alone would have fixed it.
3. **Rollback:** Cancel/timeout rollback restores TargetSnapshot and GetIntent, not only device.
4. **Recovery:** Pod restart after delete apply — deleted intent must not replay onto device.

**Acceptance:** Level 1 unit tests green in config-server + data-server; Level 2 cache-backend integration job green including new ghost-delete scenario; existing `10-srl-cache-backend.robot` cases remain green.

## Out of Scope

- Tombstone / deleted annotation on snapshot entries.
- kubectl force-delete with config still on device.
- Treating LoadAll-of-intents-not-in-RPC as a bug.
- 04-Sensitive TC5/TC6 unless proven same mechanism.
- jsonpath quoting fix (already on integration-tests branch).
- Cross-target snapshot coupling (independent per target).
- Batching multiple intents into one write RPC call per `TransactionSet` (deferred future optimization, not needed now).

## Further Notes

- **CI failure reference:** config-server actions run 32839868310, job 97803120102 — ghost `intent1` during `customer` delete teardown.
- **Config/SC deletion order:** SensitiveConfig removed in `ProcessSuccess` after Confirm — later than apply-time snapshot write; orphan-key prune on reconcile is recovery only.
- **TargetConfig does not watch TargetSnapshot** — apply-time patches do not enqueue TargetConfig.
- **Paired PRs must be updated on branch** before merge; do not merge read-path-only fix without write-path fix.
- **Round 4 (write-path mechanics) resolved 2026-09-01**, closing the gap this spec's prior revision left open ("Extend colocated config-server local API (new RPCs on existing service or sibling service)" — now settled as: merge + rename to `ConfigSnapshotService`, per-intent, payload-over-the-wire, delete hard-fails, merge-patch concurrency, orphan-loop deleted outright).
- **Tracking:** local spec only — `.scratch/last-applied-snapshot-write-at-apply/spec.md` (no GitHub issue; a prior GitHub issue, #482, was opened and then closed at the user's instruction in favor of local-only tracking).
