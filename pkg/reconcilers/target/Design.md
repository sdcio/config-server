# target reconciler design

## Goal

The target reconciler watches the `target CR` and creates a target datastore in the data-server. 

1. It selects a data-server per target (right now there is just 1 data-server)
2. It performs CRUD operations on the target data-store on the selected data-server. A change on the data-store configuration is implemented as a delete and re-create.

The datastore configuration consist of information retrieved by:
- `TargetConnectionProfile CR`
- `TargetSyncProfile CR`
- `Secret CR`
- `TLS Secret CR`


## create or update logic

1. select a data-server if not already selected
2. update data-store
    - create a datastore if none exists
    - if changes to the config are detected delete and create the data-store
    - do nothing if no changes were detected

## Validating changes

all the reference are immutable objects
- we reference the used resourceVersion in the status

## Status

a dedicated datastore condition is setup to reflect issues

## TODO

- Persist the decision of the data-server
- Implement TLS
