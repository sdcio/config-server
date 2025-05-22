package schema

import (
	"context"
)

type SchemaServerConnector interface {
	Upload(ctx context.Context, schemaFolder string, schema SchemaID, schemaLoadSpec *SchemaLoadSpec) error
	Remove(ctx context.Context, schema SchemaID) error
}
