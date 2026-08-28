# 07 — Wire `DeviceProfile` from `TargetConnectionProfile` CR through `CreateDataStore` gRPC

**What to build:** When config-server reconciles a Target that references a
`TargetConnectionProfile` with `deviceProfile: sonic` (or any other non-empty profile), that
value flows into the `CreateDataStore` gRPC call's `Target.device_profile` field — so data-server
receives it and can activate the correct NOS-specific encoder. Without this ticket, the
data-server-side wiring (data-server tickets 01, 03, 04, 05) can only be reached through static
YAML SBI config; it is unreachable through the Kubernetes control plane.

**Blocked by:** 06 (`DeviceProfile` field must exist on `TargetConnectionProfileSpec`)

**Status:** done

### sdc-protos dependency

`go.mod` must be bumped to `github.com/sdcio/sdc-protos` commit
[`40ed0bc`](https://github.com/sdcio/sdc-protos/commit/40ed0bc26a71263302a5b24b741ab67db4fac01f)
(branch `deviceprofile`, [sdc-protos#120](https://github.com/sdcio/sdc-protos/pull/120)) — this is
the commit that adds `DEVICE_PROFILE_SONIC = 2` to the `DeviceProfile` enum. The `Target` proto
message already carries `DeviceProfile device_profile = 11` (added in the same PR's first commit).

### Checklist

- [x] `go.mod` bumped to `sdc-protos@40ed0bc` (branch `deviceprofile`). Confirmed
  `sdcpb.DeviceProfile_DEVICE_PROFILE_SONIC` is visible after `go mod tidy`.
- [x] New helper in the targetdatastore reconciler package
  (`pkg/reconcilers/targetdatastore/deviceprofile.go`) that converts `inv1alpha1.DeviceProfile` →
  `sdcpb.DeviceProfile`. Takes the resolved value (`invv1alpha1.DeviceProfile`, via the existing
  `TargetConnectionProfile.DeviceProfile()` accessor which already defaults an absent
  `Spec.DeviceProfile` to `DeviceProfileNone`) rather than a pointer, to avoid a second nil-check
  duplicating what the accessor already does:
  ```go
  func toProtoDeviceProfile(dp invv1alpha1.DeviceProfile) sdcpb.DeviceProfile {
      switch dp {
      case invv1alpha1.DeviceProfileSonic:
          return sdcpb.DeviceProfile_DEVICE_PROFILE_SONIC
      case invv1alpha1.DeviceProfileCiscoIOSXR:
          return sdcpb.DeviceProfile_DEVICE_PROFILE_CISCO_IOS_XR
      default:
          return sdcpb.DeviceProfile_DEVICE_PROFILE_GENERIC
      }
  }
  ```
- [x] `getCreateDataStoreRequest` (`pkg/reconcilers/targetdatastore/reconciler.go`) reads
  `connProfile.DeviceProfile()` and sets it on the `sdcpb.Target`:
  ```go
  DeviceProfile: toProtoDeviceProfile(connProfile.DeviceProfile()),
  ```
- [x] The `ensureDatastore` call path (`pkg/sdc/target/manager/runtime.go`) is unaffected — it
  already passes the `Target` proto through opaquely; no changes made there.
- [x] Unit test covering the new mapping: no profile (`DeviceProfileNone`) → `DEVICE_PROFILE_GENERIC`;
  `"sonic"` → `DEVICE_PROFILE_SONIC`; `"cisco-ios-xr"` → `DEVICE_PROFILE_CISCO_IOS_XR`; unknown
  string → `DEVICE_PROFILE_GENERIC`.
- [x] Existing reconciliation behaviour for targets without a `deviceProfile` field is unaffected
  (absent `Spec.DeviceProfile` → `DeviceProfileNone` via the accessor → generic profile → no
  change to existing data-server behaviour).

## Comments

Landed on branch `sonic-device-profile` (commit `d29e20b`, worktree
`config-server-worktrees/sonic-device-profile`), cut from `origin/main` rather than the
`config-server-cache-backend` branch config-server's primary checkout happens to be on — that
branch carries an unrelated, still-unmerged sensitive-config-CR feature stack, and its sdc-protos
pin (`a8f3da07030a`, the `config-server-cache-backend` sdc-protos branch) predates and conflicts
with the `deviceprofile` branch's base. Ticket 06's commit was cherry-picked cleanly onto `main`
first (see ticket 06's own Comments), then this ticket's work was added on top. `sdc-protos` was
bumped straight to `40ed0bc26a71` with no cherry-picking needed, since that commit is a clean
forward descendant of `main`'s existing sdc-protos pin.
