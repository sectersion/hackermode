# hackermode registry — wire shape

A hackermode registry serves a small set of HTTP endpoints. The host
fetches a global index, per-module version listings, and platform-specific
binary tarballs through these endpoints. All responses are static — the
registry is content-addressed by SHA256 hashes recorded in the lockfile.

This document is the **contract**. The Phase C reference implementation
is a Go filesystem backend that lays the files out on disk in the same
shape; a production HTTP server (Node.js or otherwise) implements these
endpoints and may store the underlying data however it likes.

---

## Endpoints

### `GET /index.json`

Returns the global module index. Polled occasionally; cached locally.

```json
{
  "schema": "0.1",
  "modules": [
    {
      "id":        "acme.email",
      "name":      "Email",
      "description": "IMAP/SMTP email client",
      "latest":    "0.3.4",
      "tags":      ["mail", "productivity"]
    },
    ...
  ]
}
```

- `schema` — registry schema version. The client refuses to talk to
  registries it doesn't understand.
- `modules[].id` — canonical module ID (reverse-DNS style).
- `modules[].latest` — convenience; the client treats this as a hint and
  always re-fetches per-module versions before installing.

---

### `GET /module/{id}.json`

Returns metadata + every published version of a single module.

```json
{
  "id": "acme.email",
  "name": "Email",
  "description": "IMAP/SMTP email client",
  "homepage": "https://github.com/acme/hackermode-email",
  "license": "MIT",
  "author": "Acme",
  "versions": [
    {
      "version":   "0.3.4",
      "released":  "2026-05-12T00:00:00Z",
      "yanked":    false,
      "manifest_url":  "/module/acme.email/0.3.4/manifest.toml",
      "checksums_url": "/module/acme.email/0.3.4/checksums.txt",
      "binaries": [
        { "platform": "linux/amd64", "url": "/module/acme.email/0.3.4/linux-amd64.tar.gz", "sha256": "..." },
        { "platform": "darwin/arm64", "url": "/module/acme.email/0.3.4/darwin-arm64.tar.gz", "sha256": "..." }
      ],
      "dependencies": {
        "acme.oauth": "^1.0"
      },
      "capabilities": ["network", "secrets"]
    },
    {
      "version": "0.3.3",
      ...
    }
  ]
}
```

- `versions` are returned newest-first.
- `yanked` versions are excluded from new installs but still resolvable
  for an existing lockfile (so a yanked version doesn't immediately
  break someone's reproducible build — they get a warning instead).
- `manifest_url` and `checksums_url` are paths relative to the registry
  base URL. They may be absolute (e.g. a GitHub release URL) when the
  registry mirrors externally.

---

### `GET /module/{id}/{version}/manifest.toml`

Returns the module's own `hackermode.toml` (the module-side schema, not
the user-facing one). The host validates this matches what it expects
from `dependencies` and `capabilities` in the version metadata.

---

### `GET /module/{id}/{version}/checksums.txt`

Plain text, one entry per line:

```
deadbeefca... linux-amd64.tar.gz
abc12345...  darwin-arm64.tar.gz
```

Used to verify downloaded tarballs match what the registry advertised in
the `/module/{id}.json` response. (Two sources for the same data is
deliberate — defense in depth against a manipulated index.)

---

### `GET /module/{id}/{version}/{platform}.tar.gz`

The platform binary tarball. Layout inside:

```
manifest.toml
<binary-name>
LICENSE
README.md            (optional)
```

`<binary-name>` matches `[entry].binary` in the manifest. The host
extracts to `~/.local/share/hackermode/modules/{id}/{version}/` and
symlinks `current → {version}`.

---

## Filesystem layout (reference)

The Phase C filesystem-backed registry stores files at:

```
<registry-root>/
├── index.json
└── modules/
    └── acme.email/
        ├── module.json                     # per-module metadata
        ├── 0.3.4/
        │   ├── manifest.toml
        │   ├── checksums.txt
        │   ├── linux-amd64.tar.gz
        │   └── darwin-arm64.tar.gz
        └── 0.3.3/
            └── ...
```

The endpoint paths above map directly:

| Endpoint                                | File                                                        |
|------------------------------------------|-------------------------------------------------------------|
| `GET /index.json`                        | `<registry-root>/index.json`                                |
| `GET /module/acme.email.json`            | `<registry-root>/modules/acme.email/module.json`            |
| `GET /module/acme.email/0.3.4/manifest.toml` | `<registry-root>/modules/acme.email/0.3.4/manifest.toml` |
| `GET /module/acme.email/0.3.4/checksums.txt` | `<registry-root>/modules/acme.email/0.3.4/checksums.txt` |
| `GET /module/acme.email/0.3.4/linux-amd64.tar.gz` | `<registry-root>/modules/acme.email/0.3.4/linux-amd64.tar.gz` |

`hackermode registry serve <dir>` ships a tiny HTTP server that exposes
this filesystem layout via the endpoint shape. The Node.js production
implementation will need to serve the same shape (data shape, not the
filesystem layout — that's a private detail).

---

## Trust model

- Every tarball is identified by SHA256 in two places: the version
  metadata in `/module/{id}.json` and the standalone `checksums.txt`.
- The client verifies both match before extracting.
- Signature support (sigstore / minisign) lands in **Phase F**. The
  format already has room for it via additional fields on each
  `versions[]` entry; clients ignore unknown fields.

---

## Versioning this document

The wire shape is versioned with the `schema` field on `/index.json`.
Breaking changes bump the major component (`1.0`); additive changes do
not. Clients refuse registries with an unknown major.
