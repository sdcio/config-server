# 06 — Add `DeviceProfile` typed field to `TargetConnectionProfileSpec`

**What to build:** An operator creating a `TargetConnectionProfile` CR for a SONiC device can
set `deviceProfile: sonic` on it, and the K8s API (CRD schema validation) enforces the allowed
values at apply time — the same closed-set guarantee that `protocol` and `encoding` already
provide today.

**Blocked by:** None — can start immediately.

**Status:** done

- [x] New `type DeviceProfile string` declared in `apis/inv/v1alpha1/`, alongside the existing
  `Protocol` and `Encoding` type definitions in that package. Constants:
  ```go
  const (
      DeviceProfileNone        DeviceProfile = ""
      DeviceProfileSonic       DeviceProfile = "sonic"
      DeviceProfileCiscoIOSXR  DeviceProfile = "cisco-ios-xr"
  )
  ```
  Values must mirror `pkg/config.DeviceProfile` in data-server exactly, since they map 1-to-1
  through the gRPC `DeviceProfile` enum.
- [x] `DeviceProfile *DeviceProfile` field added to `TargetConnectionProfileSpec`
  (`apis/inv/v1alpha1/targetconnprofile_types.go`) with a kubebuilder validation marker:
  ```go
  // +kubebuilder:validation:Enum="";sonic;cisco-ios-xr
  DeviceProfile *DeviceProfile `json:"deviceProfile,omitempty" yaml:"deviceProfile,omitempty"`
  ```
  The field is optional (`omitempty`) — absence means `DeviceProfileNone` (generic, no
  NOS-specific encoder), matching the existing data-server default.
- [x] CRD regenerated (`make generate manifests` or equivalent) so the JSON Schema enum
  constraint appears in the generated CRD YAML.
- [x] No behavioural change in any reconciler or controller — this ticket only adds the field to
  the API type and CRD; wiring it into the gRPC call is ticket 07.
- [x] Existing `TargetConnectionProfile` resources without a `deviceProfile` field continue to
  function identically (nil pointer → `DeviceProfileNone` / generic behaviour).

## Comments

- Landed on branch `config-server-cache-backend`: added `DeviceProfile` type + `DeviceProfileNone`/
  `DeviceProfileSonic`/`DeviceProfileCiscoIOSXR` constants and the optional `DeviceProfile` field
  (with `DeviceProfile()` accessor) in `apis/inv/v1alpha1/targetconnprofile_types.go`/
  `targetconnprofile_helpers.go`, regenerated deepcopy + CRD manifests
  (`crds/inv.sdcio.dev_targetconnectionprofiles.yaml`, `artifacts/inv.sdcio.dev_targetconnectionprofiles.yaml`),
  and added a `target-conn-profile-gnmi-sonic.yaml` fixture/test case.
- Re-landed cleanly (cherry-pick, no conflicts) onto a fresh `sonic-device-profile` branch/worktree
  cut from `origin/main`, ahead of ticket 07: `config-server-cache-backend` carries an unrelated,
  still-unmerged sensitive-config-CR feature stack and its own sdc-protos pin, neither of which
  this effort should depend on. See ticket 07's Comments for why.
