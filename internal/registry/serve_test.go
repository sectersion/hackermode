package registry

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServe_EndpointsMatchWireShape(t *testing.T) {
	root := seed(t)
	srv := httptest.NewServer(NewHandler(root))
	defer srv.Close()

	cases := []struct {
		path string
		want string
	}{
		{"/index.json", `"acme.email"`},
		{"/module/acme.email.json", `"0.3.4"`},
		{"/module/acme.email/0.3.4/checksums.txt", `linux-amd64.tar.gz`},
	}
	for _, c := range cases {
		resp, err := srv.Client().Get(srv.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: status=%d body=%s", c.path, resp.StatusCode, body)
		}
		if !strings.Contains(string(body), c.want) {
			t.Fatalf("GET %s missing %q in body:\n%s", c.path, c.want, body)
		}
	}
}

func TestServe_IndexParsableAsWireShape(t *testing.T) {
	srv := httptest.NewServer(NewHandler(seed(t)))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/index.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var idx Index
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if idx.Schema != "0.1" || len(idx.Modules) == 0 {
		t.Fatalf("decoded: %+v", idx)
	}
}

func TestServe_NotFound(t *testing.T) {
	srv := httptest.NewServer(NewHandler(seed(t)))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/module/nope.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServe_MethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(NewHandler(seed(t)))
	defer srv.Close()
	req, _ := srv.Client().Head(srv.URL + "/index.json")
	if req == nil {
		t.Fatal("HEAD failed")
	}
	defer req.Body.Close()
	if req.StatusCode != 200 {
		t.Fatalf("HEAD should be allowed: %d", req.StatusCode)
	}
}
