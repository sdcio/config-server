package git

type GitRefKind string

const (
	GitRefKindTag    GitRefKind = "tag"
	GitRefKindBranch GitRefKind = "branch"
	GitRefKindHash   GitRefKind = "hash"
)
