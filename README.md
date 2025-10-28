# Config-server

![sdc logo](https://docs.sdcio.dev/assets/logos/SDC-transparent-withname-100x133.png)


The config-server is a Kubernetes-based Operator and comprises of several controllers:

- Schema Controller: Manages the lifecycle of schemas using Schema Custom Resources (CR).
- Discovery Controller: Manages the lifecycle of targets through DiscoveryRule CR, discovering devices/NF(s)
- Target Controller: Manages the lifecycle of Target DataStores using Target CR.
- Config API Server: Manages the lifecycle of Config resources.
    - Utilizes its storage backend (not etcd).
    - Interacts declaratively with the data-server through Intent transactions.
    - Implements validation checks, rejecting configurations that fail validation.

Config-server is part of Schema Driven Configuration (SDC)

The paradigm of schema-driven API approaches is gaining increasing popularity as it facilitates programmatic interaction with systems by both machines and humans. While OpenAPI schema stands out as a widely embraced system, there are other notable schema approaches like YANG, among others. This project endeavors to empower users with a declarative and idempotent method for seamless interaction with API systems, providing a robust foundation for effective system configuration."

## Join us

Have questions, ideas, bug reports or just want to chat? Come join [our discord server](https://discord.com/channels/1240272304294985800/1311031796372344894).

## License and Code of Conduct

Code is under the [Apache License 2.0](LICENSE), documentation is [CC BY 4.0](LICENSE-documentation).

The SDC project is following the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). More information and links about the CNCF Code of Conduct are [here](code-of-conduct.md).

