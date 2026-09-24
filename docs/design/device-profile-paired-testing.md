# Device profile: control-plane wiring and paired integration

This document describes how `deviceProfile` on `TargetConnectionProfile` relates to
data-server and how to run integration tests before NOS-specific data-server PRs merge.

## Control-plane behaviour (base stack)

- `TargetConnectionProfile.spec.deviceProfile` accepts `""` (generic), `sonic`, and
  `cisco-ios-xr`. The CRD enum matches `pkg/config.DeviceProfile` in data-server and the
  `sdcpb.DeviceProfile` gRPC enum (`sdc-protos#120`).
- On target reconciliation, config-server maps the CR field to `CreateDataStore` →
  `Target.device_profile`. config-server does **not** implement NOS encoders; it passes the
  value through to data-server.
- On the **device-profile base** data-server branch, `sonic` and `cisco-ios-xr` are known but
  **not enabled**: CreateDataStore fails with the same deterministic *device profile is not
  enabled* error as static data-server YAML config. Generic (empty) profile behaviour is
  unchanged.

End-to-end SONiC or Cisco IOS-XR onboarding through the K8s control plane requires the matching
**NOS data-server PR** (SONiC or Cisco stack) in addition to this config-server wiring.

## CI: pairing with data-server

Integration jobs resolve the data-server image from `Pairs-with:` lines in the pull request
body (see `.github/workflows/test.yml`).

For a config-server PR that stacks on data-server **device-profile-base**, include:

```text
Pairs-with: sdcio/data-server#<device-profile-base-PR-number>
```

Do **not** assume integration scenarios that apply intent to a live SONiC or IOS-XR device
will pass until:

| `deviceProfile`   | data-server prerequisite                          |
|-------------------|---------------------------------------------------|
| (empty / generic) | `main` or device-profile base only                |
| `sonic`           | SONiC NOS PR merged (profile enablement)        |
| `cisco-ios-xr`    | Cisco IOS-XR NOS PR merged (when no longer draft) |

Reference connection-profile fixtures (unit-tested examples) live under
`example/connection-profiles/` (`target-conn-profile-gnmi-sonic.yaml`,
`target-conn-profile-gnmi-cisco-ios-xr.yaml`). Full SONiC onboarding YAML sets are tracked on
the `sonic-device-profile` config-server branch.
