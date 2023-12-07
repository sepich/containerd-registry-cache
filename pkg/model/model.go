package model

type ObjectType string

const (
	ObjectTypeBlob     ObjectType = "blob"
	ObjectTypeManifest ObjectType = "manifest"
)

type ObjectIdentifier struct {
	Registry   string // A registry (domain name) to reach, such as quay.io. This must be where the /v2/ API is hosted.
	Repository string // A repository, e.g. prom/node-exporter. This need not be "user/repo" form and could be "user/project/repo".
	Ref        string // For manifests, this could be a tag or a digest. For blobs this will just be a digest.
	// ContentType string // Only really relevant for manifests depending on Accept header?
	Type ObjectType
}
