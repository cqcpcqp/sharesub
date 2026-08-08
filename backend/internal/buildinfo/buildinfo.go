package buildinfo

// Version and Revision are replaced with release metadata by the build pipeline.
var (
	Version  = "development"
	Revision = "unknown"
)

type Info struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

func Current() Info {
	return Info{Version: Version, Revision: Revision}
}
