# 02 — Implement `ConfigSnapshotService.Modify`/`Delete` handlers with per-key merge-patch

Status: done

**Spec:** `.scratch/last-applied-snapshot-write-at-apply/spec.md` (see "Write path (primary)" and "Concurrency" decisions)

**What to build:** Server-side handlers for the new `Modify`/`Delete` RPCs on `ConfigSnapshotService` (renamed from `ConfigReadService` in ticket 01). Both write a single key of `TargetSnapshot.Spec.Configs` via a targeted JSON merge-patch — not get-then-replace — so this apply-time write and the post-Confirm backstop prune (ticket 04) can't stomp on each other even if they race. `Modify` upserts the entry from the payload the caller sends (already encrypted); incidental fields (`Revertive`, `Lifecycle`) are filled from config-server's current `Config`/`SensitiveConfig` at write time. `Delete` removes the key outright — no tombstone.

This ticket is independently verifiable via handler unit tests + fixtures, without any data-server change.

**Blocked by:** 01 (sdc-protos rename + new RPC shapes)

- [x] `Modify` handler upserts `Spec.Configs[name]` via merge-patch from the request payload; incidental fields sourced from current `Config`/`SensitiveConfig`
- [x] `Delete` handler removes `Spec.Configs[name]` via merge-patch; missing key is a no-op success (idempotent), not an error
- [x] Both handlers return a real RPC error on kube-client failure (no log-only failure path)
- [x] Server registration (`pkg/sdc/configread/server.go`) updated for the renamed service
- [x] Unit tests in `pkg/sdc/configread/handlers_test.go` cover: modify creates/updates entry, delete removes entry, delete of missing key is a no-op, merge-patch targets only the single key (a concurrent unrelated key in the map is untouched)

## Comments

Implemented on `config-server` worktree at `/home/mava/projects/config-server-worktrees/config-server-cache-backend-race` (branch `config-server-cache-backend-race`).

- `Modify`/`Delete` write via `client.RawPatch(types.MergePatchType, ...)` targeting only `spec.configs.<name>` — RFC 7396 merge-patch semantics guarantee sibling keys are untouched, satisfying the concurrent-unrelated-key requirement without a get-then-replace round trip. `Modify` falls back to `Create` on a `NotFound` patch result (first apply for a target with no `TargetSnapshot` yet); `Delete` maps `NotFound` to a no-op success.
- Added `fromConfigEntry` (`last_applied.go`) as the write-path counterpart to the existing `toLastAppliedConfigEntry`: encrypts the wire `ConfigEntry`'s blobs via the same `KeyRing`, and sources `Revertive`/`Lifecycle` from a fresh `client.Get` of the live `SensitiveConfig` (same namespace as the `TargetSnapshot`) rather than trusting the wire entry's own `non_revertive`/`orphan` flags for those two fields, per the ticket's "incidental fields sourced from current Config/SensitiveConfig" requirement.
- `sdc-protos` had to be bumped from `a8f3da0` (pre-rename, ticket 01's parent commit) to `5a14ea0` (ticket 01's actual commit, which had never been pushed) — pushed the `last-applied-snapshot-write-at-apply` branch to `origin` and re-pinned `go.mod`/`go.sum`, since ticket 01 landed the RPC shapes locally but left the cross-repo dependency pointing at the old commit.
- Full `go build ./...` and `go test ./...` pass for the module.
