# discovery rule reconciler design

## Goal

The target reconciler watches the `DiscoveryRule CR` and creates a `target CR` as a result of the discovery. 

It uses `owner references` in the associated target CR. This means namespaces will not to be the same for discovery rule and target CR


## create or update logic

1. Check if the disocovery rule exists and if running
    - if exists -> stop and delete the discovery rule go routine
2. Get the `group`, `version` and `kind` discovery rule reference from the `DiscoveryRule CR`. This is an immutable object. This Discovery rule reference is the implementation of the discovery rule we use for the discovery. E.g. `DiscoveryRuleIPRange`
3. One we have the `GVK` we validate if this `discovery rule` implementation is supported and if so we initialize the discovery rule implementation.
- we get the `Discovery rule CR` associated with the implementation e.g. `DiscoveryRuleIPRange`.
4. We get the additional context for the discovery-rule. Such as:
- `TargetConnectionProfile`
- `TargetSyncProfile`
- `Secret`
5. We start/run the discovery rule go routine and store the discovery rule in our discoveryRule in memory store
    - DiscoveryRuleIPRange:
        - get ip host addresses
        - per ip address discover the target
            - check the protocol to be used `gnmi`, `netconf`, `ssh`, `snmp`
                - `gnmi`: 
                    - uses capabilities to check the vendor
                    - uses the speicifc vendor implementation to discover the device information (MAC, SerialNumber, Vendor, Type, etc)
                    - apply the target to etcd



## TODO

- implement other discovery protocols: netconf, ssh, snmp
- Return error from go routine and update the status
- make the rule reference immutable
- implement TLS
- why does the Own trigger so many reconcile changes-> is it becuse of the time and date







## new design

Discovery Context
- Key: APIversion:Kind:Namespace:Name:Prefix
- Discovery
    - prefix/range:
        discovered
        not discovered
    - address
        discovered
        not discovered
    - no discovery/address
- Run:
    - get addresses -> hostIP(s)
    - check discovered targets


1. gather profiles and validate profile existance (profiles are immutable) 
    -> results in profiles being valid or not
2. do we do schema validation -> TBD

-> results in a list of profiles being valid or not

3. walk over the list of ips/services/pods relevant for this discovery rule
per entry we check the

On Change: (upon reconcile -> start/stop)
- check Discovery entries and make them up to date
- stop/start the discovery loop

Periodically: ()
- Per DiscoverRule
    - Discovery Entries (prefixes and addresses and profiles)
    - Discovered Entries (static or dynamic, addresses)
        - if retry works keep the entry
        - if rety
