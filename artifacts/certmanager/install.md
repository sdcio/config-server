# cert-manager prerequisite

The config-server now relies on [cert-manager](https://cert-manager.io) to
issue the TLS certificate served by the aggregated API server, replacing the
previously hardcoded `api-server-cert` Secret.

The trust chain follows the cert-manager
[SelfSigned bootstrapping pattern](https://cert-manager.io/docs/configuration/selfsigned/#bootstrapping-ca-issuers):

```
selfsigned-cluster-issuer (ClusterIssuer, selfSigned)
        |
        v
sdc-ca (Certificate, isCA: true) -> Secret: sdc-ca-secret
        |
        v
sdc-ca-issuer (Issuer, ca)
        |
        v
api-server-cert (Certificate) -> Secret: api-server-cert
```

The `APIService` carries the annotation
`cert-manager.io/inject-ca-from: sdc-system/sdc-ca`, so cert-manager's
`cainjector` automatically populates `spec.caBundle` with the `sdc-ca`
certificate from the `sdc-ca-secret` Secret. The bundle is anchored to the CA,
so it is stable across leaf rotations and is available as
soon as `sdc-ca` is Ready.

## Install

1. Install cert-manager (must be running before the resources below are
   applied, the `cainjector` and webhook controllers are required):

   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
   ```

2. Wait for cert-manager to be ready:

   ```bash
   kubectl -n cert-manager rollout status deployment/cert-manager
   kubectl -n cert-manager rollout status deployment/cert-manager-cainjector
   kubectl -n cert-manager rollout status deployment/cert-manager-webhook
   ```

3. Bootstrap the SDC trust anchor (ClusterIssuer + CA + namespaced Issuer).
   This must be applied **after** the `sdc-system` namespace exists, because
   the CA `Certificate` and `Issuer` are namespaced:

   ```bash
   kubectl apply -f ../ns.yaml
   kubectl apply -f clusterissuer.yaml
   kubectl apply -f ca-certificate.yaml
   kubectl apply -f ca-issuer.yaml
   ```

4. Verify that the bootstrap is healthy:

   ```bash
   kubectl get clusterissuer selfsigned-cluster-issuer
   kubectl -n sdc-system get certificate sdc-ca
   kubectl -n sdc-system get issuer sdc-ca-issuer
   ```

After this, the rest of the config-server manifests (including
`certificate-apiserver.yaml` and `apiservice.yaml`) can be applied normally.
