# schema reconciler design

## Goal

The schema reconciler watches the `schema CR` and downloads the schema from a specifc URL 

schema is immutable -> change handling is not relevant

the following elements need to be aligned.
- dir of the yang schema with key provider/version
- schema of the schema-server

## create logic

1. validate if the provider/version dir exists; if not create it
2. validate if the schema in the schema server exists; if not create it

## delete logic

1. validate if the schema in the schema server exists; if yes delete it
2. validate if the provider/version dir exists; if yes delete it

## update logic

schema is an immutable object, not needed

