package aptpackage

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func directFixture(name string) Config {
	disabled := false
	return Config{Control: Control{Name: name, Version: "1.0~beta1", Architecture: "all", Maintainer: "Fixture <fixture@example.invalid>", Description: "Metadata-only fixture"}, Provenance: &disabled, Scripts: MaintainerScripts{PostInst: "#!/bin/sh\nset -e\necho fixture\n"}}
}
func TestMetadataDebStructureAndReproducibility(t *testing.T) {
	requireDebTools(t)
	c := directFixture("direct-fixture")
	c.Control.Version = "1:1.0~beta1"
	c.Control.Metadata = map[string]any{"X-Fixture": "preserved"}
	var a, b bytes.Buffer
	if err := WriteMetadataDeb(&a, c, 123); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadataDeb(&b, c, 123); err != nil || !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("not reproducible: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fixture.deb")
	if err := os.WriteFile(path, a.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("dpkg-deb", "--ctrl-tarfile", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	names := []string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		mode := int64(0644)
		if h.Name == "./postinst" {
			mode = 0755
			body, _ := io.ReadAll(tr)
			if string(body) != c.Scripts.PostInst {
				t.Fatal("script changed")
			}
		}
		if h.Uid != 0 || h.Gid != 0 || h.Mode != mode || h.ModTime.Unix() != 123 {
			t.Fatalf("bad header: %+v", h)
		}
	}
	if !reflect.DeepEqual(names, []string{"./control", "./postinst"}) {
		t.Fatal(names)
	}
	contents, err := exec.Command("dpkg-deb", "--contents", path).CombinedOutput()
	if err != nil || len(contents) != 0 {
		t.Fatalf("nonempty/invalid data tar: %q %v", contents, err)
	}
	custom, err := exec.Command("dpkg-deb", "-f", path, "X-Fixture").Output()
	if err != nil || string(custom) != "preserved\n" {
		t.Fatalf("lost custom metadata: %q %v", custom, err)
	}
	parsed, err := ParsePackage(path)
	if err != nil || parsed.Name != c.Control.Name {
		t.Fatalf("parse: %+v %v", parsed, err)
	}
	if err := WriteMetadataDeb(shortDirectWriter{}, c, 0); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("ignored short write: %v", err)
	}
	if err := WriteMetadataDeb(failingDirectWriter{}, c, 0); err == nil {
		t.Fatal("ignored output failure")
	}
}

type shortDirectWriter struct{}

func (shortDirectWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

type failingDirectWriter struct{}

func (failingDirectWriter) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }

func TestReadDefinitionsFormatsAndBounds(t *testing.T) {
	root := t.TempDir()
	a, b := directFixture("aa-package"), directFixture("bb-package")
	encodedA, _ := json.Marshal(a)
	encodedB, _ := json.Marshal(b)
	single := filepath.Join(root, "single.json")
	if err := os.WriteFile(single, encodedA, 0644); err != nil {
		t.Fatal(err)
	}
	lines := filepath.Join(root, "packages.jsonl")
	if err := os.WriteFile(lines, append(append(append([]byte{}, encodedA...), '\n'), encodedB...), 0644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "defs")
	os.Mkdir(dir, 0755)
	os.WriteFile(filepath.Join(dir, "b.json"), encodedB, 0644)
	os.WriteFile(filepath.Join(dir, "a.json"), encodedA, 0644)
	for _, path := range []string{single, lines, dir} {
		defs, err := ReadDefinitions(path)
		if err != nil {
			t.Fatal(err)
		}
		if defs[0].Control.Name != a.Control.Name {
			t.Fatal("order changed")
		}
		if path != single && (len(defs) != 2 || defs[1].Control.Name != b.Control.Name) {
			t.Fatal("bad normalized order")
		}
	}
	for _, data := range []string{`{"control":{"name":"aa","name":"bb"}}`, `{"control":{"name":"aa","Name":"bb"}}`, `{"control":{},"payload":"ignored"}`, string(encodedA) + "\n\n" + string(encodedB), strings.Repeat("x", MaxDefinitionBytes+2)} {
		path := filepath.Join(root, "bad.jsonl")
		os.WriteFile(path, []byte(data), 0644)
		if _, err := ReadDefinitions(path); err == nil {
			t.Fatal("accepted malformed/oversize input")
		}
	}
	symlink := filepath.Join(dir, "c.json")
	if err := os.Symlink(single, symlink); err == nil {
		if _, err := ReadDefinitions(dir); err == nil {
			t.Fatal("accepted symlink definition")
		}
	}
}

func TestComposeParallelHashesIndexesAndDuplicates(t *testing.T) {
	requireDebTools(t)
	defs := []Config{}
	for i := 0; i < 20; i++ {
		defs = append(defs, directFixture(fmt.Sprintf("meta-%03d", i)))
	}
	defs[0].Control.Version = "1:1.0~beta1"
	defs[1].Control.Architecture = "amd64"
	root := t.TempDir()
	first, second := filepath.Join(root, "one"), filepath.Join(root, "eight")
	cfg := Config{OutDir: first}
	if err := ComposeDefinitions(context.Background(), cfg, defs, DirectOptions{Workers: 1, MemoryBytes: 128 << 20}); err != nil {
		t.Fatal(err)
	}
	cfg.OutDir = second
	defs = append(defs, defs[0])
	if err := ComposeDefinitions(context.Background(), cfg, defs, DirectOptions{Workers: 8, MemoryBytes: 128 << 20}); err != nil {
		t.Fatal(err)
	}
	assertLifecycleTreeEqual(t, second, snapshotLifecycleTree(t, first))
	locator, _ := ParseRepositoryLocator(second)
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	report, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Workers: 4})
	if err != nil || report.PackageCount != 20 {
		t.Fatalf("report=%+v %v", report, err)
	}
	if report.Packages[0].Version != "1:1.0~beta1" {
		t.Fatal("epoch/identity changed")
	}
	artifact := filepath.Join(second, report.Packages[0].Filename)
	if err := os.WriteFile(artifact, []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRepository(context.Background(), reader, RepositoryOptions{}); err == nil {
		t.Fatal("accepted tampered direct artifact")
	}
}

func TestComposeFailureCleanupAndMemoryFallback(t *testing.T) {
	requireDebTools(t)
	t.Setenv("GNUPGHOME", t.TempDir())
	root := t.TempDir()
	good := directFixture("good-fixture")
	bad := directFixture("good-fixture")
	bad.Control.Description = "conflict"
	for _, test := range []struct {
		name string
		defs []Config
		opts DirectOptions
		cfg  Config
	}{
		{name: "conflict", defs: []Config{good, bad}},
		{name: "metadata", defs: []Config{directFixture("UPPER")}},
		{name: "filesystem", defs: []Config{good, directFixture(strings.Repeat("a", 300))}, opts: DirectOptions{Workers: 2}},
		{name: "budget", defs: []Config{good}, opts: DirectOptions{MemoryBytes: 1}},
		{name: "epoch", defs: []Config{good}, opts: DirectOptions{Epoch: -1}},
		{name: "signing", defs: []Config{good}, cfg: Config{GPG: "missing-direct-signing-fixture"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := test.cfg
			cfg.OutDir = filepath.Join(root, test.name)
			if err := ComposeDefinitions(context.Background(), cfg, test.defs, test.opts); err == nil {
				t.Fatal("expected failure")
			}
			if _, err := os.Lstat(cfg.OutDir); !os.IsNotExist(err) {
				t.Fatal("failed candidate activated")
			}
			matches, _ := filepath.Glob(filepath.Join(root, "."+test.name+".tpa-compose-*"))
			if len(matches) != 0 {
				t.Fatalf("candidate leaked: %v", matches)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ComposeDefinitions(ctx, Config{OutDir: filepath.Join(root, "cancel")}, []Config{good}, DirectOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "fallback")
	if err := ComposeDefinitions(context.Background(), Config{OutDir: destination}, []Config{good, directFixture("other-fixture")}, DirectOptions{Workers: 32, MemoryBytes: 9 << 20}); err != nil {
		t.Fatalf("single worker fallback failed: %v", err)
	}
	before := snapshotLifecycleTree(t, destination)
	if err := ComposeDefinitions(context.Background(), Config{OutDir: destination}, []Config{good}, DirectOptions{}); err == nil {
		t.Fatal("replaced existing output")
	}
	assertLifecycleTreeEqual(t, destination, before)
}

func TestComposeSignedRepository(t *testing.T) {
	requireDebTools(t)
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg missing")
	}
	home := t.TempDir()
	os.Chmod(home, 0700)
	t.Setenv("GNUPGHOME", home)
	fingerprint := generateTestSigningKey(t, "Direct Fixture <direct@example.invalid>")
	repository := filepath.Join(t.TempDir(), "repository")
	if err := ComposeDefinitions(context.Background(), Config{OutDir: repository, GPG: fingerprint}, []Config{directFixture("signed-direct")}, DirectOptions{}); err != nil {
		t.Fatal(err)
	}
	public, err := exec.Command("gpg", "--batch", "--export", fingerprint).Output()
	if err != nil {
		t.Fatal(err)
	}
	locator, _ := ParseRepositoryLocator(repository)
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	report, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Keyring: public, Fingerprint: fingerprint, RequireSigned: true})
	if err != nil || !report.SignatureVerified || report.PackageCount != 1 {
		t.Fatalf("%+v %v", report, err)
	}
}
