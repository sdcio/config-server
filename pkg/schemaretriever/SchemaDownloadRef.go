package schemaretriever

type SchemaDownloadRef struct {
	Vendor     string
	Kind       string
	Version    string
	Tag        string
	Branch     string
	Repository string
	Folders    []*CopyDef
}

type CopyDef struct {
	Src string
	Dst string
}
