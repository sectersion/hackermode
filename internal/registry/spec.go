// Package registry holds the wire types shared by every registry backend.
// The shapes here match docs/REGISTRY.md byte-for-byte (JSON tags).
//
// Splitting the wire shape into its own file means the filesystem and
// HTTP backends (and the resolver index adapter) refer to a single
// source of truth — when the production Node.js registry lands it
// implements these JSON shapes.
package registry

// SchemaVersion is the registry wire-shape version this client speaks.
const SchemaVersion = "0.1"

// Index is the top-level catalogue. Served as /index.json.
type Index struct {
	Schema  string       `json:"schema"`
	Modules []IndexEntry `json:"modules"`
}

// IndexEntry is one module in the global index. The detail comes from
// /module/{id}.json — this is just enough to surface a search list.
type IndexEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Latest      string   `json:"latest"`
	Tags        []string `json:"tags,omitempty"`
}

// Module is the per-module metadata response. Served as /module/{id}.json.
type Module struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	License     string          `json:"license,omitempty"`
	Author      string          `json:"author,omitempty"`
	Versions    []ModuleVersion `json:"versions"`
}

// ModuleVersion describes one published release.
type ModuleVersion struct {
	Version       string            `json:"version"`
	Released      string            `json:"released,omitempty"`
	Yanked        bool              `json:"yanked,omitempty"`
	ManifestURL   string            `json:"manifest_url"`
	ChecksumsURL  string            `json:"checksums_url"`
	Binaries      []BinaryEntry     `json:"binaries"`
	Dependencies  map[string]string `json:"dependencies,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
}

// BinaryEntry is one platform-specific tarball.
type BinaryEntry struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

// ChecksumsFile is the parsed form of checksums.txt. Each line is
// "sha256 filename", separated by whitespace; blank lines and comments
// starting with '#' are ignored.
type ChecksumsFile struct {
	Entries map[string]string // filename → sha256
}
