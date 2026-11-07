# Sweepers: fully managed subtrees

An intent is **revertive** when the values it owns are restored after an out-of-band change. That says nothing about configuration no intent owns: someone who connects to the device and adds configuration leaves it behind.

A **sweeper** closes that gap. It is an intent that carries `deletePaths` and no `config`. It deletes everything under its paths that is held at a lower **precedence** than its own priority. A subtree is **fully managed** when no configuration exists in it that no intent owns; you create that property by placing a sweeper on the subtree.

## Priority and precedence

A lower priority number wins. Priorities are 32-bit integers:

| Holder | Priority |
|---|---|
| User intents (settable) | `2` up to max − 500 |
| Running configuration | max − 100 |
| Defaults | max − 90 |

where max is 2147483647.

**A sweeper at priority P deletes everything under its paths held at a number higher than P.** That includes running configuration, defaults and every intent with a lower precedence (higher number) than the sweeper. Intents with a lower number than P survive.

There is no reserved sweeper priority and no check against the other intents on the target. You choose the priority and are responsible for what it removes.

## Creating a sweeper

Use an ordinary `Config` with `deletePaths` and no `config`:

```yaml
apiVersion: config.sdcio.dev/v1alpha1
kind: Config
metadata:
  name: interfaces-sweeper
  namespace: default
  labels:
    config.sdcio.dev/targetName: leaf1
    config.sdcio.dev/targetNamespace: default
spec:
  priority: 1000   # just below (a higher number than) every real intent for /interfaces
  deletePaths:
    - /interfaces
```

Exactly one of `config` and `deletePaths` must be set. Both set, or neither, is rejected at admission. Every entry must parse with the same path syntax as `config` paths; an invalid entry is rejected with its index. `ConfigSet` (in its template) and `SensitiveConfig` follow the same rules; for `SensitiveConfig` a sweeper's priority must also fit in 32 bits.

### One subtree fully managed

Put the sweeper on that subtree, with a priority just below every real intent that owns configuration there:

```yaml
deletePaths:
  - /interfaces
```

Everything else on the device stays free-form.

### The whole device fully managed

```yaml
deletePaths:
  - /
```

> **Warning.** A sweeper at `/` removes everything held at a higher number than its priority, including the configuration that keeps the device reachable (management access, credentials, routes to the controller) if the intents that provide it sit at a higher number than the sweeper. Make sure the intents that keep management open have a lower priority number than the sweeper before creating it.

## Behaviour

- The sweeper is sent in the same transaction as the other intents of its target.
- The data-server re-asserts the deletes on every transaction, so out-of-band additions under the paths are removed again on the next reconcile.
- Changing `deletePaths` takes effect on the next reconcile.
- When a datastore is recovered, the sweeper is resent from the applied config in status (`status.appliedConfig.deletePaths`), not from the current spec.
- Applied `deletePaths` are visible in `status.appliedConfig`.
- Deleting the sweeper removes only its own delete markers. It does not delete configuration from the device.
- A dry run of a sweeper goes through the same path and previews the sweep.

## `revertive`

The sweeper is re-applied with `revertive` unset and with `revertive: false`: a data-server test confirms that out-of-band configuration under its paths is deleted in both cases. The `revertive` setting therefore has no effect on the delete part of a sweeper; the explicit deletes are re-asserted on every transaction either way.

## `SensitiveConfig`

The component that resolves secrets and applies `SensitiveConfig` lives outside this repository. This repository adds the API field and validation only; that component must forward `deletePaths` as deletes on the transaction intent.
