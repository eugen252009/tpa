package aptpackage

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDebControlReaderFormatsMatchDPKG(t *testing.T) {
	requireDebTools(t)
	control := strings.Replace(basicControl("reader-fixture", "1.2.3", "all"),
		"Description: repository fixture\n",
		"Description: metadata reader fixture\n"+
			" Long description continuation line one.\n"+
			" Another continuation line with UTF-8: café.\n", 1) +
		"Depends: base-package (>= 1.0), other-package\n" +
		"Provides: virtual-reader (= 1.2.3)\n" +
		"Future-Field: retained metadata\n"
	for _, fixture := range []struct {
		name        string
		compression string
		direct      bool
	}{
		{name: "xz", compression: "xz", direct: true},
		{name: "gzip", compression: "gzip", direct: true},
		{name: "uncompressed", compression: "none", direct: true},
		{name: "zstd fallback", compression: "zstd", direct: false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			deb := buildDebWithCompression(t, t.TempDir(), fixture.compression, control, false)
			direct, directErr := readPackageControlDirect(deb)
			if fixture.direct {
				if directErr != nil {
					t.Fatalf("direct reader: %v", directErr)
				}
			} else {
				var unsupported *unsupportedDebFormatError
				if !errors.As(directErr, &unsupported) {
					t.Fatalf("direct reader error = %v, want unsupported format", directErr)
				}
			}
			got, err := readPackageControl(deb)
			if err != nil {
				t.Fatalf("read package control: %v", err)
			}
			oracle, err := readPackageControlWithDPKG(deb)
			if err != nil {
				t.Fatalf("dpkg-deb oracle: %v", err)
			}
			assertControlEquivalent(t, got, oracle)
			if fixture.direct && !bytes.Equal(direct, got) {
				t.Fatalf("direct path and selected path differ")
			}
		})
	}
}

func TestXZControlReaderCapsDictionaryForSmallBlock(t *testing.T) {
	requireDebTools(t)
	deb := buildDebWithCompression(t, t.TempDir(), "xz", basicControl("dict-cap", "1", "all"), false)
	file, err := os.Open(deb)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	members, _, err := readDebAR(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	var control debARMember
	for _, member := range members {
		if member.name == "control.tar.xz" {
			control = member
		}
	}
	if control.size == 0 {
		t.Fatal("fixture has no control.tar.xz")
	}
	reader, err := prepareXZReader(file, control.offset, control.size)
	if err != nil {
		t.Fatal(err)
	}
	patched, ok := reader.(*xzPatchedReader)
	if !ok || len(patched.patches) == 0 {
		t.Fatalf("expected bounded XZ header patches, got %T", reader)
	}
	if _, err := readPackageControlDirect(deb); err != nil {
		t.Fatalf("read package with bounded XZ dictionary: %v", err)
	}
}

func TestDebControlReaderMinimalPackage(t *testing.T) {
	requireDebTools(t)
	deb := buildDebWithCompression(t, t.TempDir(), "xz", basicControl("minimal", "1", "all"), true)
	control, err := readPackageControlDirect(deb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseControl(control); err != nil {
		t.Fatalf("parse minimal control: %v", err)
	}
}

func TestDebControlReaderRejectsMalformedPackagesWithoutFallback(t *testing.T) {
	requireDebTools(t)
	valid := buildDebWithCompression(t, t.TempDir(), "xz", basicControl("bad-reader", "1", "all"), false)
	validBytes, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	members, _, err := readDebAR(bytes.NewReader(validBytes), int64(len(validBytes)))
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func([]byte) []byte{
		"malformed ar magic": func(data []byte) []byte { return []byte("not an ar archive") },
		"truncated ar":       func(data []byte) []byte { return data[:len(data)-1] },
		"bad debian-binary": func(data []byte) []byte {
			copy(data[members[0].offset:members[0].offset+members[0].size], []byte("x.y\n"))
			return data
		},
		"malformed control compression": func(data []byte) []byte {
			for _, member := range members {
				if member.name == "control.tar.xz" {
					data[member.offset] ^= 0xff
					break
				}
			}
			return data
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data := append([]byte(nil), validBytes...)
			path := filepath.Join(t.TempDir(), "bad.deb")
			if err := os.WriteFile(path, mutate(data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := readPackageControlDirect(path)
			if err == nil {
				t.Fatal("direct reader accepted malformed package")
			}
			var unsupported *unsupportedDebFormatError
			if errors.As(err, &unsupported) {
				t.Fatalf("malformed package was marked fallback-eligible: %v", err)
			}
			if _, err := readPackageControl(path); err == nil {
				t.Fatal("selected reader accepted malformed package")
			}
		})
	}
}

func TestDebControlReaderRejectsMissingControlAndMalformedTar(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		controlTar []byte
	}{
		{name: "missing control file", controlTar: makeTar(t, nil)},
		{name: "malformed tar", controlTar: bytes.Repeat([]byte{0x41}, 1024)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data := makeTestAR([]testARMember{
				{name: "debian-binary", data: []byte("2.0\n")},
				{name: "control.tar", data: fixture.controlTar},
				{name: "data.tar", data: makeTar(t, nil)},
			})
			path := filepath.Join(t.TempDir(), "bad.deb")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readPackageControlDirect(path); err == nil {
				t.Fatal("direct reader accepted malformed control archive")
			}
		})
	}
	missingControl := makeTestAR([]testARMember{
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "_future", data: []byte("extension")},
		{name: "data.tar", data: makeTar(t, nil)},
	})
	path := filepath.Join(t.TempDir(), "missing-control.deb")
	if err := os.WriteFile(path, missingControl, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPackageControlDirect(path); err == nil || !strings.Contains(err.Error(), "missing control.tar") {
		t.Fatalf("missing control.tar error = %v", err)
	}
}

func TestControlTarRejectsMissingMarkersAndNonzeroTrailer(t *testing.T) {
	control := []byte(basicControl("tail-check", "1", "all"))
	var truncated bytes.Buffer
	writer := tar.NewWriter(&truncated)
	if err := writer.WriteHeader(&tar.Header{Name: "control", Mode: 0o644, Size: int64(len(control)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(control); err != nil {
		t.Fatal(err)
	}
	if _, err := readControlTar(bytes.NewReader(truncated.Bytes())); err == nil {
		t.Fatal("control tar without terminal zero blocks was accepted")
	}

	valid := makeTar(t, map[string][]byte{"control": control})
	// Trailing tar padding must remain zero after the two end blocks.
	valid = append(valid, bytes.Repeat([]byte{0}, 512)...)
	valid[len(valid)-512] = 1
	if _, err := readControlTar(bytes.NewReader(valid)); err == nil {
		t.Fatal("control tar with nonzero trailer was accepted")
	}
}

func TestDebControlReaderCorpusOracle(t *testing.T) {
	dir := os.Getenv("TPA_DEB_CORPUS")
	if dir == "" {
		t.Skip("set TPA_DEB_CORPUS to compare the retained package corpus against dpkg-deb")
	}
	requireDebTools(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var directElapsed, oracleElapsed time.Duration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".deb") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		started := time.Now()
		got, err := readPackageControlDirect(path)
		directElapsed += time.Since(started)
		if err != nil {
			t.Fatalf("direct read %s: %v", entry.Name(), err)
		}
		started = time.Now()
		oracle, err := readPackageControlWithDPKG(path)
		oracleElapsed += time.Since(started)
		if err != nil {
			t.Fatalf("dpkg-deb oracle %s: %v", entry.Name(), err)
		}
		assertControlEquivalent(t, got, oracle)
		count++
	}
	if count == 0 {
		t.Fatalf("no .deb files found in %s", dir)
	}
	t.Logf("direct reader matched dpkg-deb for %d packages (direct=%s, dpkg-deb=%s)", count, directElapsed, oracleElapsed)
}

func FuzzDebARReader(f *testing.F) {
	f.Add([]byte("!<arch>\n"))
	f.Add([]byte("not an ar archive"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxControlArchiveSize {
			t.Skip()
		}
		_, _, _ = readDebAR(bytes.NewReader(data), int64(len(data)))
	})
}

func FuzzControlTarReader(f *testing.F) {
	f.Add([]byte{})
	f.Add(makeTarForFuzz())
	f.Add(bytes.Repeat([]byte{0}, 1024))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxControlArchiveSize {
			t.Skip()
		}
		_, _ = readControlTar(bytes.NewReader(data))
	})
}

func buildDebWithCompression(t *testing.T, dir, compression, control string, emptyData bool) string {
	t.Helper()
	requireDebTools(t)
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(root, "DEBIAN"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DEBIAN", "control"), []byte(control), 0o644); err != nil {
		t.Fatal(err)
	}
	if !emptyData {
		if err := os.MkdirAll(filepath.Join(root, "usr", "share", "tpa-test"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "usr", "share", "tpa-test", "payload"), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deb := filepath.Join(dir, "fixture.deb")
	cmd := exec.Command("dpkg-deb", "--build", "--root-owner-group", "-Z"+compression, root, deb)
	if output, err := cmd.CombinedOutput(); err != nil {
		if compression == "zstd" && strings.Contains(strings.ToLower(string(output)), "unknown compression") {
			t.Skipf("dpkg-deb does not support zstd fixture: %s", output)
		}
		t.Fatalf("dpkg-deb -Z%s: %v: %s", compression, err, output)
	}
	return deb
}

func assertControlEquivalent(t *testing.T, got, want []byte) {
	t.Helper()
	gotControl, err := ParseControl(got)
	if err != nil {
		t.Fatalf("parse direct control: %v", err)
	}
	wantControl, err := ParseControl(want)
	if err != nil {
		t.Fatalf("parse oracle control: %v", err)
	}
	if !reflect.DeepEqual(gotControl, wantControl) {
		t.Fatalf("normalized package metadata differs:\ndirect: %+v\noracle: %+v", gotControl, wantControl)
	}
	gotStanza, err := repositoryControlStanza(got)
	if err != nil {
		t.Fatalf("direct stanza: %v", err)
	}
	wantStanza, err := repositoryControlStanza(want)
	if err != nil {
		t.Fatalf("oracle stanza: %v", err)
	}
	if !bytes.Equal(gotStanza, wantStanza) {
		t.Fatalf("repository control stanza differs:\ndirect: %q\noracle: %q", gotStanza, wantStanza)
	}
}

type testARMember struct {
	name string
	data []byte
}

func makeTestAR(members []testARMember) []byte {
	var archive bytes.Buffer
	archive.WriteString("!<arch>\n")
	for _, member := range members {
		header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8s%-10d`\n", member.name+"/", 0, 0, 0, "100644", len(member.data))
		if len(header) != 60 {
			panic("invalid test ar header length")
		}
		archive.WriteString(header)
		archive.Write(member.data)
		if len(member.data)%2 != 0 {
			archive.WriteByte('\n')
		}
	}
	return archive.Bytes()
}

func makeTar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for name, data := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeTarForFuzz() []byte {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	data := []byte("Package: fuzz\nVersion: 1\nArchitecture: all\nMaintainer: Fuzz\nDescription: Fuzz\n")
	_ = writer.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR})
	_, _ = writer.Write(data)
	_ = writer.Close()
	return buffer.Bytes()
}
