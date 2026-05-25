package manifest

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockfile_RoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	l := &Lockfile{
		Meta: LockMeta{
			ManifestHash: "sha256:abc",
			Generated:    now,
			Hackermode:   ">=0.1",
		},
		Modules: []LockModule{
			{
				ID:      "acme.email",
				Version: "0.3.4",
				Source:  "registry",
				SHA256:  "deadbeef",
				Binaries: []LockBinary{
					{Platform: "linux/amd64", SHA256: "aaa", URL: "https://x/l-a.tgz"},
					{Platform: "darwin/arm64", SHA256: "bbb", URL: "https://x/d-a.tgz"},
				},
			},
		},
	}
	out, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseLock(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	if back.Meta.ManifestHash != "sha256:abc" {
		t.Fatalf("manifest hash lost: %+v", back.Meta)
	}
	if len(back.Modules) != 1 || back.Modules[0].ID != "acme.email" {
		t.Fatalf("modules: %+v", back.Modules)
	}
	if len(back.Modules[0].Binaries) != 2 {
		t.Fatalf("binaries: %+v", back.Modules[0].Binaries)
	}
}

func TestLockfile_MarshalSortsDeterministic(t *testing.T) {
	l := &Lockfile{
		Modules: []LockModule{
			{ID: "z.last", Source: "registry"},
			{ID: "a.first", Source: "registry"},
			{ID: "m.mid", Source: "registry", Binaries: []LockBinary{
				{Platform: "linux/amd64"},
				{Platform: "darwin/arm64"},
			}},
		},
	}
	out, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	idxA := strings.Index(s, `"a.first"`)
	idxM := strings.Index(s, `"m.mid"`)
	idxZ := strings.Index(s, `"z.last"`)
	if !(idxA < idxM && idxM < idxZ) {
		t.Fatalf("modules not sorted:\n%s", s)
	}
	if strings.Index(s, "darwin/arm64") > strings.Index(s, "linux/amd64") {
		t.Fatalf("binaries not sorted:\n%s", s)
	}
}

func TestLockfile_UpsertAndRemove(t *testing.T) {
	l := NewLockfile("/tmp/x.lock")
	l.Upsert(LockModule{ID: "a", Version: "1.0.0"})
	l.Upsert(LockModule{ID: "a", Version: "1.0.1"}) // replace
	if got, _ := l.Get("a"); got.Version != "1.0.1" {
		t.Fatalf("expected replacement, got %q", got.Version)
	}
	l.Upsert(LockModule{ID: "b", Version: "2.0.0"})
	if len(l.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(l.Modules))
	}
	l.Remove("a")
	if _, ok := l.Get("a"); ok {
		t.Fatal("expected a to be removed")
	}
	l.Remove("missing") // no-op
}

func TestLockfile_WriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	l := NewLockfile(filepath.Join(dir, "hackermode.lock"))
	l.Meta.ManifestHash = "sha256:test"
	l.Upsert(LockModule{ID: "x", Version: "0.1.0", Source: "registry"})
	if err := l.Write(); err != nil {
		t.Fatal(err)
	}
	loaded, err := ParseLockFile(l.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Meta.ManifestHash != "sha256:test" {
		t.Fatalf("manifest hash: %q", loaded.Meta.ManifestHash)
	}
	if got, _ := loaded.Get("x"); got.Version != "0.1.0" {
		t.Fatalf("module: %+v", got)
	}
}

func TestLockfilePathFor(t *testing.T) {
	cases := map[string]string{
		"/repo/hackermode.toml":           "/repo/hackermode.lock",
		"/home/u/.config/hackermode/profiles/work.toml": "/home/u/.config/hackermode/profiles/work.lock",
	}
	for in, want := range cases {
		if got := LockfilePathFor(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestLockfile_WriteEmptyPathFails(t *testing.T) {
	l := &Lockfile{}
	if err := l.Write(); err == nil {
		t.Fatal("expected error")
	}
}
