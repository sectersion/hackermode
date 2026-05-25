// Package registry — HTTP server that exposes a filesystem registry over
// the wire endpoints documented in docs/REGISTRY.md.
//
// The `hackermode registry serve <dir>` subcommand uses this. Production
// will run a Node.js implementation of the same endpoints; this Go
// version exists so:
//
//   - Module authors can test their release artifacts locally before
//     publishing.
//   - CI can spin up an ephemeral registry against a temp directory.
//   - Examples and tutorials don't depend on any external service.
//
// The server is read-only. Publishing flows write to the filesystem
// directly (Phase C5).
package registry

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewHandler returns an http.Handler that serves the registry rooted at
// `root`. The handler is safe to use behind any net/http server.
//
// Routing follows the wire shape:
//
//   GET /index.json
//   GET /module/{id}.json
//   GET /module/{id}/{version}/manifest.toml
//   GET /module/{id}/{version}/checksums.txt
//   GET /module/{id}/{version}/{platform}.tar.gz
//
// Every other path returns 404. The filesystem layout under <root>
// matches FS exactly; we just rewrite a few URLs (notably the global
// "/module/{id}.json" → "modules/{id}/module.json").
func NewHandler(root string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, filepath.Join(root, "index.json"), "application/json")
	})

	mux.HandleFunc("/module/", func(w http.ResponseWriter, r *http.Request) {
		// Strip the "/module/" prefix and route by shape.
		rest := strings.TrimPrefix(r.URL.Path, "/module/")
		parts := strings.Split(rest, "/")
		switch {
		// "/module/{id}.json"
		case len(parts) == 1 && strings.HasSuffix(parts[0], ".json"):
			id := strings.TrimSuffix(parts[0], ".json")
			if id == "" {
				http.NotFound(w, r)
				return
			}
			serveFile(w, r,
				filepath.Join(root, "modules", id, "module.json"),
				"application/json")

		// "/module/{id}/{version}/<file>"
		case len(parts) == 3:
			id, version, file := parts[0], parts[1], parts[2]
			if id == "" || version == "" || file == "" {
				http.NotFound(w, r)
				return
			}
			ct := contentTypeFor(file)
			serveFile(w, r, filepath.Join(root, "modules", id, version, file), ct)
		default:
			http.NotFound(w, r)
		}
	})

	return mux
}

func serveFile(w http.ResponseWriter, r *http.Request, path, ct string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeFile(w, r, path)
}

func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".toml"):
		return "text/x-toml; charset=utf-8"
	case strings.HasSuffix(name, ".txt"):
		return "text/plain; charset=utf-8"
	case strings.HasSuffix(name, ".tar.gz"):
		return "application/gzip"
	default:
		return ""
	}
}

// ServeAddr is a convenience wrapper that starts an HTTP server with
// NewHandler at the given address. Blocks until the server exits.
func ServeAddr(addr, root string) error {
	srv := &http.Server{Addr: addr, Handler: NewHandler(root)}
	fmt.Fprintf(os.Stderr, "hackermode registry serving %s on http://%s\n", root, addr)
	return srv.ListenAndServe()
}
