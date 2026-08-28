# SONiC device onboarding (reference)

Reference KRM manifests for onboarding a SONiC device (`sonic_yang` origin, via a custom
translib-write-enabled `telemetry` binary) through sdcio's Kubernetes control plane, end to end.
These are configuration artifacts only — no code changes are required to use them.

## Prerequisites

- schema-server running and reachable by the config-server controller.
- The target device runs a `telemetry` binary built with `gnmi_translib_write` (stock SONiC's
  `telemetry` binary does not support gNMI Set against `sonic_yang`).
- `deviceProfile: sonic` requires `encoding: JSON_IETF` — data-server rejects any other encoding
  for this profile at config-load time.
- Get/Subscribe against `sonic_yang` cannot use a bare `/` path (translib hard-errors with
  `"Path is empty"`); `sync-profile.yaml` below lists one path per top-level module instead. Extend
  that list for any additional modules you intend to manage.

## Files

| File | Kind | Purpose |
|------|------|---------|
| `schema.yaml` | `Schema` | Loads the `sonic_yang` YANG models (+ IETF types) into schema-server. |
| `connection-profile.yaml` | `TargetConnectionProfile` | gNMI + `JSON_IETF` + `deviceProfile: sonic`. |
| `sync-profile.yaml` | `TargetSyncProfile` | Per-module Get/Subscribe paths (no root `/`). |
| `discovery-rule.yaml` | `DiscoveryRule` + `Secret` | Discovers the device and references the profiles above; includes the basic-auth credentials `Secret` it points at. |

## Apply order

```bash
kubectl apply -f schema.yaml
kubectl apply -f connection-profile.yaml
kubectl apply -f sync-profile.yaml
kubectl apply -f discovery-rule.yaml
```

`schema.yaml` and the profiles have no ordering dependency on each other, but `discovery-rule.yaml`
references both by name (`defaultSchema`, `targetConnectionProfiles[].connectionProfile`/
`syncProfile`) and must be applied last.

## Adjust before use

- `schema.yaml`: `version`/`ref` pin the `sonic-schema` branch; bump alongside SONiC image upgrades.
- `connection-profile.yaml`: `port` (device's gNMI listener) and `insecure`/`skipVerify` for your
  TLS posture.
- `sync-profile.yaml`: the per-module `paths` lists are a starting point — add every top-level
  `sonic_yang` module container you plan to manage.
- `discovery-rule.yaml`: `addresses` (or switch to `prefixes` for subnet-based discovery) and the
  `sonic-credentials` Secret's `username`/`password`.

## Further reading

The data-server side of this work (the SONiC `GnmiSetPlan` encoder, closed-set `device-profile`
config validation, and the dispatch in `materialize.BuildPlan`) is tracked in the sibling
`sdcio/data-server` repo's `.scratch/sonic-device-profile/` effort. A user-guide page under
`iptecharch/docs` (`docs/user-guide/configuration/target/device-profiles.md`) is planned separately
and will supersede this README as the canonical operator-facing doc once it lands.
