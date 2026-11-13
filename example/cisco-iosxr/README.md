# Cisco IOS-XR examples

| File | Kind | Purpose |
|------|------|---------|
| `sync-profile.yaml` | `TargetSyncProfile` | gNMI Get sync for interfaces, static routes, BGP, and OSPF (`cisco-ixr-sync`). |
| `config-static-route.yaml` | `Config` | Sample IPv4 static route under `Cisco-IOS-XR-um-router-static-cfg`. |
| `config-bgp.yaml` | `Config` | Sample local AS and eBGP neighbor under `Cisco-IOS-XR-um-router-bgp-cfg`. |
| `config-loopback0.yaml` | `Config` | Loopback0 /32 (apply before `config-ospf.yaml` if the interface does not exist). |
| `config-ospf.yaml` | `Config` | Sample OSPF process and area under `Cisco-IOS-XR-um-router-ospf-cfg`. |

Related: `example/schemas/schema-cisco-iosxr-7.10.1.yaml`, `example/discoveryvendor-profile/discoveryvendor-profile-cisco-iosxr.yaml`.

Update `config.sdcio.dev/targetName` on the Config resources to match your `Target`. After applying config on the device, confirm sync data includes the corresponding subtrees under each `/Cisco-IOS-XR-um-router-*-cfg:router` path.

### gnmic smoke test (IOS-XR requires `json_ietf`)

```bash
gnmic -a 172.20.20.2:57400 -u "$USER" -p "$PASS" --insecure -e json_ietf get \
  --encoding json_ietf --path /Cisco-IOS-XR-um-router-bgp-cfg:router

gnmic -a 172.20.20.2:57400 -u "$USER" -p "$PASS" --insecure -e json_ietf set \
  --update-path /Cisco-IOS-XR-um-router-bgp-cfg:router \
  --update-file bgp-router.json
```

Empty BGP/OSPF subtrees may return gNMI `NotFound` until the first successful Set.
