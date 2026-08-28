# Index: sonic-device-profile (config-server)

Config-server-side tickets for SONiC device onboarding. Upstream data-server tickets live in
`sdcio/data-server/.scratch/sonic-device-profile/issues/INDEX.md`.

Ticket list in dependency order. Each ticket's `Status:` line lives in its own file — this index
is a read-only summary, not the source of truth for status.

| # | Ticket | Blocked by | Status |
|---|--------|-----------|--------|
| [06](06-add-deviceprofile-to-connection-profile-spec.md) | Add `DeviceProfile` typed field to `TargetConnectionProfileSpec` | None | done |
| [07](07-wire-deviceprofile-through-grpc.md) | Wire `DeviceProfile` from CR through `CreateDataStore` gRPC | 06 | pending |
| [08](08-reference-krm-yamls.md) | Reference KRM YAMLs for SONiC device onboarding | 06 | pending |

## Working the frontier

1. Scan the table above (re-reading each ticket's own `Status:` line, since that's authoritative,
   not this table) for tickets that are `ready-for-agent` **and** unblocked (every ticket listed in
   its "Blocked by" is `Status: done`).
2. Among those, pick the lowest-numbered one — that's the frontier ticket.
3. On completion, set that ticket's `Status:` line to `done`, update its row in the table above to
   match, and append a one-line pointer under a `## Comments` heading in the ticket file noting
   what landed (commit/branch if applicable).
4. If no ticket is both `ready-for-agent` and unblocked, report that the frontier is empty instead
   of guessing.
