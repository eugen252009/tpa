package tpautils_test

import (
	"context"
	"encoding/json"
	"github.com/eugen252009/tpa/internals/aptpackage"
	"github.com/eugen252009/tpa/pkg/tpautils"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserForOrdinaryAPTRepository(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb missing")
	}
	if err := tpautils.ValidateControl(tpautils.Control{}); err == nil {
		t.Fatal("incomplete metadata accepted")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	off := false
	cfg := aptpackage.Config{Control: aptpackage.Control{Name: "utility-fixture", Version: "1.0", Architecture: "all", Maintainer: "Fixture", Description: "<script>unsafe</script>"}, Provenance: &off}
	cfg.OutDir = filepath.Join(root, "tree")
	if err := aptpackage.InitPackage(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.InDir = cfg.OutDir
	cfg.OutDir = filepath.Join(root, "debs", "fixture.deb")
	if err := aptpackage.Build(cfg); err != nil {
		t.Fatal(err)
	}
	if err := aptpackage.Pack(aptpackage.Config{InDir: filepath.Join(root, "debs"), OutDir: repo}); err != nil {
		t.Fatal(err)
	}
	// Canonical TPA sidecars must never be overwritten by the interactive format.
	if err := tpautils.GenerateBrowser(context.Background(), repo, tpautils.BrowserOptions{}); err == nil {
		t.Fatal("overwrote canonical TPA browser")
	}
	os.Remove(filepath.Join(repo, "index.html"))
	os.Remove(filepath.Join(repo, "repository.json"))
	release := filepath.Join(repo, "dists", "stable", "Release")
	before, err := os.ReadFile(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := tpautils.GenerateBrowser(context.Background(), repo, tpautils.BrowserOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(release)
	if string(before) != string(after) {
		t.Fatal("modified APT Release")
	}
	server := httptest.NewServer(http.FileServer(http.Dir(repo)))
	defer server.Close()
	for _, path := range []string{"index.html", "repository.json", "assets/app.js", "assets/style.css"} {
		resp, err := http.Get(server.URL + "/" + path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s HTTP %d", path, resp.StatusCode)
		}
		switch path {
		case "index.html":
			if !strings.Contains(string(data), "<noscript>") || !strings.Contains(string(data), "assets/app.js") {
				t.Fatal("missing browser fallback/assets")
			}
		case "repository.json":
			var view tpautils.BrowserIndex
			if err := json.Unmarshal(data, &view); err != nil || view.Format != "tpa-utils-repository-browser" || view.Version != 1 || len(view.Repository.Packages) != 1 {
				t.Fatalf("%+v %v", view, err)
			}
		case "assets/app.js":
			if strings.Contains(string(data), "innerHTML") || !strings.Contains(string(data), "textContent") {
				t.Fatal("unsafe JS rendering")
			}
		}
	}
	if err := tpautils.GenerateBrowser(context.Background(), repo, tpautils.BrowserOptions{}); err == nil {
		t.Fatal("overwrote existing website")
	}
	out := filepath.Join(root, "standalone")
	if err := tpautils.GenerateBrowser(context.Background(), repo, tpautils.BrowserOptions{Output: out, BaseURL: "javascript:alert(1)"}); err == nil {
		t.Fatal("unsafe base accepted")
	}
	if err := tpautils.GenerateBrowser(context.Background(), server.URL, tpautils.BrowserOptions{Output: out, BaseURL: server.URL + "/"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tpautils.GenerateBrowser(ctx, repo, tpautils.BrowserOptions{Output: filepath.Join(root, "cancelled"), BaseURL: server.URL}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
