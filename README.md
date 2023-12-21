# Config-server

The config-server is a Kubernetes-based Operator and comprises of several controllers:

- Schema Controller: Manages the lifecycle of schemas using Schema Custom Resources (CR).
- Discovery Controller: Manages the lifecycle of targets through DiscoveryRule CR, discovering devices/NF(s)
- Target Controller: Manages the lifecycle of Target DataStores using Target CR.
- Config API Server: Manages the lifecycle of Config resources.
    - Utilizes its storage backend (not etcd).
    - Interacts declaratively with the data-server through Intent transactions.
    - Implements validation checks, rejecting configurations that fail validation.
