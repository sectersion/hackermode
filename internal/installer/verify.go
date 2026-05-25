// Package installer — tarball verification + extraction.
//
// Two responsibilities:
//
//   1. Hash a tarball stream while consuming it (single read).
//   2. Extract a gzip+tar stream safely (no path traversal, executable
//      bits preserved for the module binary).
//
// We deliberately do NOT trust the registry's claimed SHA256 alone —
// the same hash is also stored in checksums.txt on disk, and the
// installer cross-checks both before activation.
package installer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hashCopy streams r through a sha256 writer into w. Returns the hex
// digest after copying all bytes. The reader is consumed but not closed.
func hashCopy(w io.Writer, r io.Reader) (string, error) {
	h := sha256.New()
	mw := io.MultiWriter(w, h)
	if _, err := io.Copy(mw, r); err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractTarGz unpacks the gzip+tar payload at src into dst. The
// extraction is sandboxed:
//
//   - Entry paths are cleaned and rejected if they escape dst.
//   - Symlinks are rejected (Phase F may allow them under a flag).
//   - File modes are taken from the archive; the directory mode is
//     forced to 0o755 so the host can always list.
//
// dst is created (mkdir -p) before extraction.
func extractTarGz(dst string, src io.Reader) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("mkdir dst: %w", err)
	}
	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target,
				os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
				os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("symlink/hardlink in tarball not allowed: %q", hdr.Name)
		default:
			// Skip unknown types (PAX headers, etc.).
		}
	}
}

// safeJoin returns filepath.Join(base, rel) only if the result stays
// inside base. Otherwise returns an error.
func safeJoin(base, rel string) (string, error) {
	rel = filepath.Clean(rel)
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe tar entry: %q", rel)
	}
	target := filepath.Join(base, rel)
	relCheck, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(relCheck, "..") {
		return "", fmt.Errorf("tar entry escapes base: %q", rel)
	}
	return target, nil
}
