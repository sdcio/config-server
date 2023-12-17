# schema reconciler design

## Goal

The schema reconciler watches the `schema CR` and downloads the schema from a specifc URL 

schema is immutable

## create logic

There is no update since the spec is immutable
- when a schema gets created, we check if it is already installed; if not -> install it
- once it is installed we check if the schema server has it and if not -> create/reload schema in schemaserver

## Validating changes

schema is an immutable object, not needed


## TODO

- Delete
