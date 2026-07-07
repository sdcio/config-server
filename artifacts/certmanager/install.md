# cert-manager

The config-server uses [cert-manager](https://cert-manager.io) to issue the
TLS certificate served by the aggregated API server. Install cert-manager on
the cluster before applying the SDC manifests.

## Install cert-manager

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml

kubectl -n cert-manager rollout status deploy/cert-manager
kubectl -n cert-manager rollout status deploy/cert-manager-cainjector
kubectl -n cert-manager rollout status deploy/cert-manager-webhook
```


## Trust chain

SDC ships an in-cluster PKI built on cert-manager's
[self-signed bootstrapping pattern](https://cert-manager.io/docs/configuration/selfsigned/#bootstrapping-ca-issuers):

- A cluster-scoped self-signed `ClusterIssuer` bootstraps the chain.
- It signs the SDC root CA — an `isCA: true` `Certificate` (`sdc-ca`) stored
  in the `sdc-ca-secret` Secret.
- A namespaced `Issuer` (`sdc-ca-issuer`) consumes that secret to sign SDC
  workload certificates, including the api-server's serving cert.
- The aggregated `APIService` is annotated with
  `cert-manager.io/inject-ca-from`. cert-manager's `cainjector` keeps its
  `spec.caBundle` in sync with the SDC root CA, so kube-apiserver can
  validate the aggregated API server when proxying requests to it.

Renewals (both leaf and CA) are managed by cert-manager.
