# Spec: Resolver never retries after a failed resolution if the secret round-trips to the same content

**Status:** done

**Found via:** `sdcio/data-server` PR #471 CI (run 33882234033, job `integration-tests / setup-clab-cluster-and-test`), `04-Sensitive.10-Srl-Sensitive` suite, `TC5: Missing Secret Sets ConfigResolverFailed, Last-Good SC Preserved`. Diagnosed entirely from CI artifacts (robot output XML, `data-server`/`api-server` pod logs) — no live cluster needed to confirm the mechanism; a live cluster would be needed to verify the fix end-to-end.

**Not the same mechanism as** `last-applied-snapshot-write-at-apply` (ghost intents / apply-time `TargetSnapshot` writes) — that spec explicitly listed "04-Sensitive TC5/TC6 unless proven same mechanism" as out of scope, and this write-up is the proof it's a different, unrelated bug: this one lives entirely in `configresolver`'s change detection and has nothing to do with `TargetSnapshot`/last-applied tracking or the `config-server` cache backend feature. (Confirmed independently: the `data-server` side of TC5/TC6's failure showed up identically on both the `cache_local` and `cache_config-server` CI matrix jobs — the cache-backend feature isn't implicated.)

## Problem

`Config`'s `Resolver` condition, once set to `False` (e.g. a referenced `Secret` is missing), never recovers on its own even after the missing secret is recreated with identical content — it stays frozen at the same `lastTransitionTime` and message indefinitely, well past any watch/requeue trigger. Confirmed from the `04-sensitive-out.xml` CI artifact: `Resolver`'s `lastTransitionTime` does not move for the entire polling window after `kubectl apply`-ing the deleted secret back, even with a `force-reconcile` annotation on the `Config`.

## Root cause

`reconciler.go`'s `detectChange` decides whether to re-run resolution by comparing the *current* secret key hash against `SensitiveConfig.Spec.SecretKeyHashes` — a snapshot written only on the **last successful** resolution (`save()` is never called on failure; see `Reconcile`'s comment: "Resolution failed... preserve the last good SC"). If resolution then fails (secret deleted), that stored snapshot is untouched. If the secret is later recreated with **byte-identical content** (the common case — same YAML re-applied), the freshly-fetched hash matches that stale "last known good" baseline again, `detectChange` reports `noChange()`, and `Reconcile` returns early without ever calling `resolveConfig` again. The `Resolver` condition is therefore stuck at whatever it was during the failure, forever — the Config/Secret watches and the `force-reconcile` annotation all correctly re-trigger `Reconcile`, but `detectChange`'s hash-only baseline can't tell "verified unchanged since last success" apart from "unchanged since a failure that was never retried."

## Fix

`detectChange` now also forces `configChanged = true` (the existing "force full resolution" signal) whenever the `Config`'s live `Resolver` condition is currently `False`. This makes the hash-based check advisory only when the last reconcile actually succeeded; any live failure always gets an unconditional retry on the next trigger, regardless of hash match. `Status.GetCondition` defaults an *absent* condition to `Status: False` (see `ConditionedStatus.GetCondition`), so the check guards with `HasCondition` first — otherwise every `Config` that has never run the resolver (no secrets yet) would look "failed" and force resolution on every reconcile.

- [x] `pkg/reconcilers/configresolver/reconciler.go`: `detectChange` forces `configChanged` when `Resolver` is explicitly `False`.
- [x] Unit tests: `Test_detectChange/Resolver_condition_currently_False_->_forces_configChanged_even_with_matching_hashes` and the `..._True_->_hash_match_still_means_no_change` control case (`pkg/reconcilers/configresolver/resolver_test.go`).

## Out of scope / follow-up

- **TC6** (`Recovery Via TargetSnapshot After Pod Restart`) is a separate, still-open issue: on config-server pod restart, the observed `TransactionSet` request to data-server explicitly carried `"delete":true` for **both** sensitive intents, including `intent-sensitive-srl-2` whose secrets were never touched by TC5. That's not this mechanism (confirmed: data-server did exactly what it was told, so there is no data-server-side bug here) — it points at something in the recovery/cold-start reconcile path (possibly informer-cache-not-synced-yet at boot) deciding to delete healthy, resolved intents. Left for a follow-up investigation once this fix is verified to unblock TC5 in CI.
