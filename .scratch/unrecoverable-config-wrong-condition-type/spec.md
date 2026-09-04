# Spec: fix `updateConfigWithError`'s non-recoverable branch writing the wrong condition Type

**Status:** ready-for-agent

Companion spec: `sdcio/data-server` `.scratch/netconf-midflight-eof-not-connected/spec.md`. That spec closes the data-server-side gap that causes NETCONF-restart-during-edit-config to be misclassified as non-recoverable in the first place. This spec fixes a second, independent bug: even when a `TransactionSet` error *is* correctly classified non-recoverable (by design, for genuinely unrecoverable errors, or today, incidentally, for the misclassified NETCONF EOF case before the companion spec lands), the resulting `Config` status is silently wrong.

## Problem Statement

User report: "NF Restart during edit-config (NETCONF): ... No retry/transaction is triggered after the NF recovers, leaving it with outdated configuration while status indicators (Target/Config/ConfigSet) still incorrectly report Ready=True."

`Transactor.updateConfigWithError` (`pkg/sdc/target/manager/transactor.go:341-404`) is the function that writes a `Config`'s status after a failed `TransactionSet`. It branches on `recoverable`:

```363:376:pkg/sdc/target/manager/transactor.go
	var newConfigCond condv1alpha1.Condition
	if recoverable {
		newConfigCond = configv1alpha1.ConfigFailed(msg)
	} else {
		newMessage := condv1alpha1.UnrecoverableMessage{
			ResourceVersion: current.GetResourceVersion(),
			Message:         msg,
		}
		newmsg, err := json.Marshal(newMessage)
		if err != nil {
			return err
		}
		newConfigCond = condv1alpha1.FailedUnRecoverable(string(newmsg))
	}
```

`configv1alpha1.ConfigFailed` (`apis/config/v1alpha1/condition.go:85-93`) stamps `Type: ConditionTypeConfigReady` ("ConfigReady") — correct, and consistent with `configv1alpha1.ConfigReady`/`Creating`/`Updating`, all of which use the same Type.

`condv1alpha1.FailedUnRecoverable` (`apis/condition/v1alpha1/condition.go:210-218`) is a *generic* helper (package `apis/condition/v1alpha1`, not `apis/config/v1alpha1`) that stamps `Type: ConditionTypeReady` — the top-level, umbrella "Ready" type — **not** `"ConfigReady"`. This is the only call site of `FailedUnRecoverable` in the repo (confirmed via full-repo search), so it's safe to change without touching other callers.

Every consumer of "is this Config's own device-apply state OK" keys off `ConditionTypeConfigReady` specifically, not the generic `Ready`:

- `Config.IsRecoverable(ctx)` (`apis/config/config_helpers.go:62-78`) reads `r.GetCondition(ConditionTypeConfigReady)` and checks its `Reason`/embedded `UnrecoverableMessage.ResourceVersion`.
- `Config.IsConfigConditionReady()` (`apis/config/config_helpers.go:80-83`, and the `v1alpha1` twin) reads the same condition's `.Status`.
- `configv1alpha1.GetOverallCondition` / `SetOverallStatus` (`apis/config/v1alpha1/config_helpers.go:276-340`) compute the aggregate `Ready` as `cfgC := r.GetCondition(ConditionTypeConfigReady); tgtC := r.GetCondition(ConditionTypeTargetForConfigReady); ready := cfgC.IsTrue() && tgtC.IsTrue()`.

Because `newConfigCond` in the non-recoverable branch has the wrong `Type`, the real `ConfigReady` condition on the object is **never touched** by this call. Tracing what happens next in `updateConfigWithError`:

```378:396:pkg/sdc/target/manager/transactor.go
	// Compute the new overall Ready without mutating current.
	tmp := current.DeepCopy()
	tmp.SetConditions(newConfigCond)
	newOverallCond := configv1alpha1.GetOverallCondition(tmp)
	...
	statusApply := configv1alpha1apply.ConfigStatus().
		WithConditions(condv1alpha1.DedupeConditions(newConfigCond, newOverallCond)...)
```

1. `tmp.SetConditions(newConfigCond)` sets `tmp`'s **top-level `Ready`** condition to the `FailedUnRecoverable` value (wrong slot) — `tmp`'s `ConfigReady` condition is left untouched, at whatever it was before (e.g. `True`, if the config was previously applied successfully).
2. `GetOverallCondition(tmp)` recomputes purely from `tmp.ConfigReady` (stale `True`) && `tmp.TargetForConfigReady` (also untouched, `True` if the target's transport is healthy) → returns `Ready=True`.
3. `DedupeConditions(newConfigCond, newOverallCond)` — both have `Type: "Ready"` — dedupes by Type, **last value wins** (`apis/condition/v1alpha1/condition.go:238-251`), so the persisted status keeps `newOverallCond` (`Ready=True`) and silently drops `newConfigCond` (the `FailedUnRecoverable` marker) entirely.

Net effect: the `Config`'s persisted `status.conditions` end up with `ConfigReady=True` (stale, unchanged) and `Ready=True` (recomputed from that stale value) — **the failure vanishes from status.** This matches the report's "Config ... incorrectly report Ready=True" precisely.

It also breaks the "Unrecoverable" feature's own retry-suppression logic in a second way: on the next `Target` reconcile, `getConfigsToTransact` (`pkg/sdc/target/manager/transactor.go:566-638`) calls `cfg.IsRecoverable(ctx)`, which returns `true` (default) because `ConfigReady`'s `Reason` was never set to `"Unrecoverable"`. Combined with `IsConfigConditionReady()==true` (stale `True`) and — assuming the `Config`'s `Spec` hasn't changed — `AppliedConfig`'s stored sha still matching the current `Spec` sha, the `Config` is classified `configsNoChnge` and `continue`d, i.e. treated as "already correctly applied, nothing to do." It is silently excluded from every future `TransactionSet` attempt. `Transact()` returns `(false, nil)` ("nothing to update") on every subsequent reconcile.

`ConfigSet`'s own `Ready` (`pkg/reconcilers/configset/reconciler.go: ensureConfigs`/`determineOverallStatus`) is a pure aggregation of its child `Config`s' `Ready` conditions, so it inherits the same false `True` with no additional bug needed there — this explains the report's mention of `ConfigSet` alongside `Config`.

`Target`'s `Ready` is a *separate*, not-buggy signal: `pkg/datastore/target/netconf/nc.go`'s `reconnect()` genuinely does re-establish the transport session after the EOF, so `Target`'s `TargetConnectionReady`/overall `Ready` legitimately becomes `True` again. The report's inclusion of `Target` in the "incorrectly Ready=True" list reflects that this legitimate transport-health signal is easy to conflate with "config was successfully re-applied," which — per the two bugs above — it was not.

There is also a compounding factor from a **previously fixed, unrelated bug**: `.scratch/targetconfig-stale-not-ready-after-transient-flap/spec.md`'s fix is already live in `pkg/reconcilers/targetconfig/reconciler.go:246-256` — the "nothing to transact" branch now unconditionally calls `SetConfigsTargetConditionForTarget(..., TargetForConfigReady("target ready"))` and recomputes `SetOverallStatus` on every no-op reconcile. This is correct behavior on its own, but it means that once a `Config` is stuck in the broken state described above, **every subsequent reconcile actively re-stamps and reconfirms the false `Ready=True`**, rather than merely leaving a stale value in place — making the bug more persistent/visible, not less.

## Solution

Add a `Config`-specific "unrecoverable" condition constructor that stamps the correct `Type` (matching `ConfigFailed`/`ConfigReady`), and use it in place of the generic `condv1alpha1.FailedUnRecoverable` in `updateConfigWithError`.

## User Stories

1. As an operator, when a `TransactionSet` fails for a genuinely unrecoverable reason (e.g. a malformed intent), I want the `Config`'s `Ready`/`ConfigReady` status to actually reflect `False` with `Reason: Unrecoverable`, so that I can tell the difference between "still converging" and "needs my intervention" from the CR status alone.
2. As a config-server maintainer, I want `Config.IsRecoverable(ctx)` to correctly return `false` after such a failure (until the `Config`'s spec is edited, per its existing `ResourceVersion`-gated design), so that `getConfigsToTransact`'s non-recoverable classification and retry-suppression actually take effect instead of silently no-oping.
3. As a reviewer, I want confirmation that this fix cannot itself introduce a new false-negative (a genuinely-healthy `Config` incorrectly marked `Unrecoverable`), so I don't have to independently re-derive `GetOverallCondition`'s truth table before approving.
4. As a `ConfigSet` consumer, I want its aggregate `Ready` to correctly reflect `False` when any child `Config` is stuck `Unrecoverable`, so that I don't have to separately fix anything in `pkg/reconcilers/configset/` — this should fall out automatically once the child `Config`'s own condition is correct.

## Implementation Decisions

- Add to `apis/config/v1alpha1/condition.go`, alongside `ConfigFailed`/`ConfigReady` (same file, same pattern):
  ```go
  // ConfigFailedUnrecoverable returns a condition that indicates the config
  // failed to apply for a reason considered unrecoverable without a spec
  // change. msg is expected to already be the marshaled condv1alpha1.UnrecoverableMessage
  // JSON payload, matching IsRecoverable's unmarshal expectations.
  func ConfigFailedUnrecoverable(msg string) condv1alpha1.Condition {
      return condv1alpha1.Condition{Condition: metav1.Condition{
          Type:               string(ConditionTypeConfigReady),
          Status:             metav1.ConditionFalse,
          LastTransitionTime: metav1.Now(),
          Reason:             string(condv1alpha1.ConditionReasonUnrecoverable),
          Message:            msg,
      }}
  }
  ```
  Reuse `condv1alpha1.ConditionReasonUnrecoverable` (already defined, `apis/condition/v1alpha1/condition.go:41`) as the `Reason` value — this is what `Config.IsRecoverable(ctx)` (`apis/config/config_helpers.go:65`) checks for; only the `Type` was wrong, not the `Reason` string.
- In `pkg/sdc/target/manager/transactor.go`, `updateConfigWithError` (line 376): replace
  ```go
  newConfigCond = condv1alpha1.FailedUnRecoverable(string(newmsg))
  ```
  with
  ```go
  newConfigCond = configv1alpha1.ConfigFailedUnrecoverable(string(newmsg))
  ```
  No other changes to this function are needed: the `UnrecoverableMessage` JSON construction (lines 367-374), the `tmp.SetConditions(newConfigCond)` / `GetOverallCondition` recompute (lines 380-382), and the `DedupeConditions` call (line 393) all become correct automatically once `newConfigCond`'s `Type` matches `ConfigReady` — `SetConditions` will now overwrite the actual `ConfigReady` slot, `GetOverallCondition` will see the real `False`/`Unrecoverable` value and correctly compute overall `Ready=False`, and `DedupeConditions(newConfigCond, newOverallCond)` will now dedupe two *different*-Type conditions (`ConfigReady` and `Ready`) into two preserved entries, exactly as the recoverable branch already does today.
- Do **not** modify the generic `condv1alpha1.FailedUnRecoverable` (`apis/condition/v1alpha1/condition.go`) itself — it's a legitimate, correctly-shaped generic helper for resources whose own top-level `Ready` *is* the thing being marked unrecoverable (there may be future callers for other CRD kinds); the bug is specifically that `Config`'s `Ready` is a computed aggregate of `ConfigReady`+`TargetForConfigReady`, not a directly-settable condition, so `Config` needs its own kind-specific constructor the same way it already has `ConfigFailed`/`ConfigReady` instead of reusing the generic `Failed`/`Ready`.
- No changes needed in `pkg/reconcilers/configset/` or `apis/config/config_helpers.go` (the internal, non-versioned `Config.IsRecoverable`) — both already correctly key off the `ConditionTypeConfigReady` string, which is now populated correctly by construction.

## Testing Decisions

- New test in `apis/config/v1alpha1/` (e.g. `condition_test.go` or extend `configset_helpers_test.go`'s package): assert `ConfigFailedUnrecoverable(msg).Type == string(ConditionTypeConfigReady)` and `.Reason == string(condv1alpha1.ConditionReasonUnrecoverable)`.
- Extend/add a test in `pkg/sdc/target/manager/` (new `transactor_test.go`, or add to the existing `runtime_test.go` if it already has fake-client scaffolding suitable for `Transactor`) that:
  1. Seeds a `Config` with `ConfigReady=True` (simulating a prior successful apply) and `TargetForConfigReady=True`.
  2. Calls `updateConfigWithError(ctx, cfg, msg, err, recoverable=false)`.
  3. Asserts, on the resulting object: `ConditionTypeConfigReady` is now `False` with `Reason: Unrecoverable`; the top-level `Ready` condition is also `False` (not stale `True`); `cfg.IsRecoverable(ctx)` returns `false` immediately after (same `ResourceVersion`), and `true` again once `ResourceVersion` is bumped (simulating a spec edit) — covering the existing `IsRecoverable` contract end-to-end through this fixed code path.
  4. A companion case with `recoverable=true` asserting the existing `ConfigFailed` behavior is unchanged (regression guard).
- Extend `pkg/sdc/target/manager/getConfigsToTransact`'s existing coverage (check for an existing test file first, e.g. `pkg/sdc/target/manager/utils_test.go`/`transactor_test.go`; add one if none exists) with a case: a `Config` marked via the fixed `updateConfigWithError` non-recoverable path is classified into `nonRecoverable` (not silently treated as `configsNoChnge`) on the next `getConfigsToTransact` call.
- Final check: `go build ./...`, `go vet ./...`, `go test ./...` all pass.

## Out of Scope

- Any change to `isRecoverableTransactionError`'s gRPC-code allowlist (`pkg/sdc/target/manager/utils.go:69-82`) — that classification logic is correct as designed; this spec only fixes what happens to the status *after* something is (correctly or incorrectly) classified non-recoverable.
- Retrying "stuck" `Unrecoverable` `Config`s automatically without a spec/`ResourceVersion` change — that gating is intentional existing design (`Config.IsRecoverable`'s `ResourceVersion` check), not something this spec should relax. The companion data-server spec is what prevents the NETCONF-restart-during-edit-config case from reaching this path at all going forward.
- The `~103s`-vs-`5s` `RequeueAfter` delay investigation from `.scratch/targetconfig-stale-not-ready-after-transient-flap/spec.md` — unrelated, already tracked separately.
- Any change to `pkg/reconcilers/configset/reconciler.go` — its aggregation logic is already correct; it only needed its input (`Config.Ready`) to stop being wrong.

## Further Notes

- This bug is independent of, and predates, the companion `data-server` spec's fix — it affects *any* `TransactionSet` error that config-server classifies non-recoverable, not just misclassified NETCONF EOFs. Fix it regardless of whether/when the data-server companion spec lands.
- Once both specs land: a genuine, permanently-unrecoverable error (e.g. a malformed intent) will now correctly and durably show `Config` `Ready=False`/`Reason=Unrecoverable` (this spec), while a transient NETCONF-restart-during-edit-config EOF will be classified recoverable in the first place and retried via the existing backoff path (`pkg/reconcilers/targetconfig/reconciler.go:220-229`) without ever reaching the `Unrecoverable` branch (companion spec) — closing the reported gap from both directions.
