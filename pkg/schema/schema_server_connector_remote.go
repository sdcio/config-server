package schema

import (
	"context"

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

type SchemaServerConnectorRemote struct {
	scc sdcpb.SchemaServerClient
}

var _ SchemaServerConnector = (*SchemaServerConnectorRemote)(nil)

func NewSchemaUploaderRemote() *SchemaServerConnectorRemote {
	return &SchemaServerConnectorRemote{}
}

func (s *SchemaServerConnectorRemote) Upload(ctx context.Context, schemaFolder string, schema SchemaID, schemaLoadSpec *SchemaLoadSpec) error {
	uploadClient, err := s.scc.UploadSchema(ctx)
	if err != nil {
		return err
	}

	err = uploadClient.Send(
		&sdcpb.UploadSchemaRequest{
			Upload: &sdcpb.UploadSchemaRequest_CreateSchema{
				CreateSchema: &sdcpb.CreateSchemaRequest{
					Schema: &sdcpb.Schema{
						Vendor:  schema.provider,
						Version: schema.version,
					},
					File:      schemaLoadSpec.Models,   // ATTENTION: not sure what kind of path needs to be provided here
					Directory: schemaLoadSpec.Includes, // ATTENTION: not sure what kind of path needs to be provided here
					Exclude:   schemaLoadSpec.Excludes, // ATTENTION: not sure what kind of path needs to be provided here
				},
			},
		},
	)
	if err != nil {
		return err
	}

	panic("SchemaServerConnectorRemote not implemented yet.")

	// // for files in folders do

	// err = uploadClient.Send(&sdcpb.UploadSchemaRequest{
	// 	Upload: &sdcpb.UploadSchemaRequest_SchemaFile{
	// 		SchemaFile: &sdcpb.UploadSchemaFile{
	// 			FileName: "foo",
	// 			FileType: sdcpb.UploadSchemaFile_MODULE,
	// 			Contents: []byte{},
	// 			Hash: &sdcpb.Hash{
	// 				Method: sdcpb.Hash_MD5,
	// 				Hash: []byte{},
	// 			},

	// 		},
	// 	},
	// })

	// // finally end schema creation
	// err = uploadClient.Send(&sdcpb.UploadSchemaRequest{
	// 	Upload: &sdcpb.UploadSchemaRequest_Finalize{
	// 		Finalize: &sdcpb.UploadSchemaFinalize{},
	// 	},
	// })

	// // close the upload client ... not sure if this is correct
	// err = uploadClient.CloseSend()
	// if err != nil {
	// 	return err
	// }
	// return nil
}

func (s *SchemaServerConnectorRemote) Remove(ctx context.Context, schema SchemaID) error {
	panic("SchemaServerConnectorRemote not implemented yet.")
}
