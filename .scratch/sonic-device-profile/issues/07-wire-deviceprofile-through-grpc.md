# 07 — Wire `DeviceProfile` from `TargetConnectionProfile` CR through `CreateDataStore` gRPC

**What to build:** When config-server reconciles a Target that references a
`TargetConnectionProfile` with `deviceProfile: sonic` (or any other non-empty profile), that
value flows into the `CreateDataStore` gRPC call's `Target.device_profile` field — so data-server
receives it and can activate the correct NOS-specific encoder. Without this ticket, the
data-server-side wiring (data-server tickets 01, 03, 04, 05) can only be reached through static
YAML SBI config; it is unreachable through the Kubernetes control plane.

**Blocked by:** 06 (`DeviceProfile` field must exist on `TargetConnectionProfileSpec`)

**Status:** pending

### sdc-protos dependency

`go.mod` must be bumped to `github.com/sdcio/sdc-protos` commit
[`40ed0bc`](https://github.com/sdcio/sdc-protos/commit/40ed0bc26a71263302a5b24b741ab67db4fac01f)
(branch `deviceprofile`, [sdc-protos#120](https://github.com/sdcio/sdc-protos/pull/120)) — this is
the commit that adds `DEVICE_PROFILE_SONIC = 2` to the `DeviceProfile` enum. The `Target` proto
message already carries `DeviceProfile device_profile = 11` (added in the same PR's first commit).

### Checklist

- [ ] `go.mod` bumped to `sdc-protos@40ed0bc` (branch `deviceprofile`). Confirm
  `sdcpb.DeviceProfile_DEVICE_PROFILE_SONIC` is visible after `go mod tidy`.
- [ ] New helper (or inline mapping) in the targetdatastore reconciler package
  (`pkg/reconcilers/targetdatastore/`) that converts `inv1alpha1.DeviceProfile` →
  `sdcpb.DeviceProfile`:
  ```go
  func toProtoDeviceProfile(dp *invv1alpha1.DeviceProfile) sdcpb.DeviceProfile {
      if dp == nil {
          return sdcpb.DeviceProfile_DEVICE_PROFILE_GENERIC
      }
      switch *dp {
      case invv1alpha1.DeviceProfileSonic:
          return sdcpb.DeviceProfile_DEVICE_PROFILE_SONIC
      case invv1alpha1.DeviceProfileCiscoIOSXR:
          return sdcpb.DeviceProfile_DEVICE_PROFILE_CISCO_IOS_XR
      default:
          return sdcpb.DeviceProfile_DEVICE_PROFILE_GENERIC
      }
  }
  ```
- [ ] `getCreateDataStoreRequest` (`pkg/reconcilers/targetdatastore/reconciler.go`, currently
  ~L357–453) reads `connProfile.Spec.DeviceProfile` and sets it on the `sdcpb.Target`:
  ```go
  target.DeviceProfile = toProtoDeviceProfile(connProfile.Spec.DeviceProfile)
  ```
- [ ] The `ensureDatastore` call path (`pkg/sdc/target/manager/runtime.go`) is unaffected — it
  already passes the `Target` proto through opaquely; no changes needed there.
- [ ] Unit test covering the new mapping: nil → `DEVICE_PROFILE_GENERIC`; `"sonic"` →
  `DEVICE_PROFILE_SONIC`; `"cisco-ios-xr"` → `DEVICE_PROFILE_CISCO_IOS_XR`; unknown string →
  `DEVICE_PROFILE_GENERIC`.
- [ ] Existing reconciliation behaviour for targets without a `deviceProfile` field is unaffected
  (nil pointer on the spec → generic profile → no change to existing data-server behaviour).
