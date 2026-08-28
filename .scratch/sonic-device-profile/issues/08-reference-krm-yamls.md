# 08 — Reference KRM YAMLs for SONiC device onboarding

**What to build:** A self-contained set of reference Kubernetes resource manifests (checked in
under `example/sonic/` or `docs/sonic/` in the config-server repo) that an operator can apply to
onboard a SONiC device through sdcio's K8s control plane end-to-end. No code changes — these are
reference configuration artifacts only.

**Blocked by:** 06 (`deviceProfile` field must exist on `TargetConnectionProfileSpec` before the
`TargetConnectionProfile` YAML is accurate)

**Status:** pending

---

## Resources to produce

### 1. `Schema` CR

Loads `sonic_yang` YANG models from git into schema-server. Two repositories are required:

```yaml
apiVersion: inv.sdcio.dev/v1alpha1
kind: Schema
metadata:
  name: sonic-202605
  namespace: default
spec:
  provider: sonic.sdcio.dev
  version: "202605"
  repositories:
    - repoURL: https://github.com/sdcio/sonic-schema
      kind: branch
      ref: "202605"
      dirs:
        - src: .
          dst: sonic
      schema:
        models:
          - sonic/*.yang
        excludes:
          - sonic/sonic-extension.yang   # adjust to actual excludes verified in lab
    - repoURL: https://github.com/YangModels/yang
      kind: branch
      ref: main
      dirs:
        - src: standard/ietf/RFC
          dst: ietf
      schema:
        includes:
          - ietf/*.yang
```

Verify the `excludes` list against what `sonic-schema@202605` actually requires (consult
`/home/mava/projects/sonic/workspace/sonic-schema.yaml` for the authoritative exclusion list used
in the live lab).

---

### 2. `TargetConnectionProfile` CR

Sets gNMI + `JSON_IETF` encoding + `deviceProfile: sonic`. This is the connection template
that Target CRs and DiscoveryRules reference by name.

```yaml
apiVersion: inv.sdcio.dev/v1alpha1
kind: TargetConnectionProfile
metadata:
  name: sonic-gnmi
  namespace: default
spec:
  connectRetry: 10s
  timeout: 10s
  protocol: gnmi
  port: 57400
  encoding: JSON_IETF
  deviceProfile: sonic
  insecure: true            # adjust for TLS-enabled deployments
```

`encoding: JSON_IETF` is mandatory for the sonic device-profile — data-server rejects any other
encoding at config-load time (data-server ticket 01).

---

### 3. `TargetSyncProfile` CR

**Do not use a bare `/` path** — translib hard-errors (`"Path is empty"`) when given a root path.
List one path per top-level `sonic_yang` module container. The list below is a representative
starting point; extend it for any additional modules in scope.

```yaml
apiVersion: inv.sdcio.dev/v1alpha1
kind: TargetSyncProfile
metadata:
  name: sonic-sync
  namespace: default
spec:
  validate: true
  buffer: 0
  workers: 10
  sync:
    - name: sonic-srv6
      protocol: gnmi
      encoding: JSON_IETF
      paths:
        - /SRV6_MY_LOCATORS
        - /SRV6_MY_SIDS
      mode: onChange
    - name: sonic-bgp
      protocol: gnmi
      encoding: JSON_IETF
      paths:
        - /BGP_GLOBALS
        - /BGP_NEIGHBOR
        - /BGP_PEER_GROUP
      mode: onChange
    - name: sonic-interface
      protocol: gnmi
      encoding: JSON_IETF
      paths:
        - /PORT
        - /INTERFACE
        - /LOOPBACK_INTERFACE
      mode: onChange
```

Operators must extend the path list for every `sonic_yang` module they intend to manage. A future
iteration will add schema-driven auto-expansion of `/` to eliminate this manual list
(data-server spec: out of scope for the current iteration).

---

### 4. `DiscoveryRule` CR

References `sonic-gnmi` (the `TargetConnectionProfile` above) and `sonic-sync`. The
`deviceProfile` flows transitively through the connection profile — no new field is needed on
`DiscoveryRule` itself.

```yaml
apiVersion: inv.sdcio.dev/v1alpha1
kind: DiscoveryRule
metadata:
  name: sonic-discovery
  namespace: default
spec:
  period: 1m
  concurrentScans: 10
  defaultSchema:
    provider: sonic.sdcio.dev
    version: "202605"
  addresses:
    - address: 172.20.20.43    # replace with actual lab/prod address range
      hostName: sonic01
  targetConnectionProfiles:
    - credentials: sonic-secret   # Secret holding username/password
      connectionProfile: sonic-gnmi
      syncProfile: sonic-sync
```

For subnet-based discovery, replace `addresses` with `prefixes`:
```yaml
  prefixes:
    - prefix: 172.20.20.0/24
      excludes:
        - 172.20.20.1/32
```

---

## Checklist

- [ ] `example/sonic/` (or `docs/sonic/`) directory created in the config-server repo.
- [ ] `schema.yaml` — Schema CR as above, with confirmed `excludes` from live lab.
- [ ] `connection-profile.yaml` — `TargetConnectionProfile` CR as above.
- [ ] `sync-profile.yaml` — `TargetSyncProfile` CR with per-module paths, annotated with
  the "do not use `/`" warning.
- [ ] `discovery-rule.yaml` — `DiscoveryRule` CR as above, with inline comments on
  credentials secret format.
- [ ] Short `README.md` in the same directory: apply order (Schema → profiles → DiscoveryRule),
  prerequisites (schema-server running, translib-write-enabled `telemetry` binary on device),
  and a pointer to the data-server user-guide device-profiles page (tracked separately in the
  data-server docs PR).

## Comments
