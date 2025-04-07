package schema

import (
	"context"

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

type SchemaServerConnectorLocal struct {
	scc sdcpb.SchemaServerClient
}

var _ SchemaServerConnector = (*SchemaServerConnectorLocal)(nil)

func NewSchemaUploaderLocal(scc sdcpb.SchemaServerClient) *SchemaServerConnectorLocal {
	return &SchemaServerConnectorLocal{
		scc: scc,
	}
}

func (s *SchemaServerConnectorLocal) Upload(ctx context.Context, schemaFolder string, schema SchemaID, schemaLoadSpec *SchemaLoadSpec) error {
	_, err := s.scc.CreateSchema(ctx, &sdcpb.CreateSchemaRequest{
		Schema: &sdcpb.Schema{
			Vendor:  schema.provider,
			Version: schema.version,
		},
		File:      schemaLoadSpec.Models,
		Directory: schemaLoadSpec.Includes,
		Exclude:   schemaLoadSpec.Excludes,
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *SchemaServerConnectorLocal) Remove(ctx context.Context, schema SchemaID) error {
	_, err := s.scc.DeleteSchema(ctx, &sdcpb.DeleteSchemaRequest{
		Schema: schema.ToSdcpbSchema(),
	})
	return err
}
