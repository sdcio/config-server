# Single-container data-server pod

The controller embeds the data-server core and serves the data-server gRPC API itself. The `data-server-controller` StatefulSet runs one container, `controller`, instead of a `controller` plus a `data-server` sidecar.

## What changed

| Before | After |
|---|---|
| Two containers: `controller` and `data-server` | One container: `controller` |
| Controller dials the sidecar over gRPC | Controller calls the core in-process |
| `LOCAL_DATASERVER` env var on the controller | Removed |
| `dataServerImage` in `configmap-input-vars.yaml` | Removed |
| `ENABLE_ROLLOUT` on the controller Deployment | Removed |
| `:56000` served by the sidecar | `:56000` served by the controller container |
| Probes on the controller container only | `/readyz` and `/healthz` on `HEALTH_PROBE_PORT` (8090), including startup |

The `data-server` Service, port `56000` and the `schema-db` / `schema-store` PersistentVolumeClaims are unchanged. The `cache` volume stays an `emptyDir`.

## Upgrading

The upgrade is a single jump. There is no mixed mode: a new controller does not dial a sidecar, and no intermediate two-container release exists.

1. Update the input variables: drop `dataServerImage`, keep `sdcControllerImage` and `sdcApiServerImage`.
2. Apply the updated manifests together with the new controller and api-server images.
3. The StatefulSet pod is replaced once. The controller and the data-server are down together for the duration of the restart, and the pod reports ready only after the core has started.

The `schema-db` and `schema-store` PVCs are reused, so schemas are not downloaded or parsed again. The cache is an `emptyDir`, so the controller re-establishes the target datastores after the restart.

### Rollback

Redeploy the previous manifests and images. The PVCs stay in place.

## Operating the merged pod

- The controller container is the only container in the pod. Logs, `kubectl exec` and resource limits all address `controller`.
- `datactl` and other sdcpb clients reach the data-server through a port-forward:

  ```shell
  kubectl -n sdc-system port-forward svc/data-server 56000:56000
  datactl -a localhost:56000 datastore get --ds <target>
  ```

  `datactl` ships with [sdcio/data-server](https://github.com/sdcio/data-server) and is unchanged.
- Inside the cluster, the api-server and other clients keep using `data-server.sdc-system.svc:56000`.
- Metrics, including the gRPC server metrics, are exposed on the controller's metrics port.

## Failure coupling

The controller and the data-server core share one process and one heap. A data-server crash or an out-of-memory kill takes the controller down, and the pod restarts. This is intended: the pair fails fast and recovers together. No `GOMEMLIMIT` or cache cap is set by the manifests; size the container memory limit for both.

## Standalone data-server

The standalone `ghcr.io/sdcio/data-server` image is still published for development, CLI use and tests. Production clusters no longer deploy it next to the controller.
