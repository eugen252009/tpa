package aptpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPackageRejectsInvalidNamesBeforeCreatingFiles(t *testing.T) {
	for _, name := range []string{"myNewAPTPackage", "UPPER", "", "a", "-example", "example_name", "../example", "example name", "éxample"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "package")
			err := InitPackage(Config{OutDir: root, Control: Control{Name: name}})
			if err == nil || !strings.Contains(err.Error(), "https://www.debian.org/doc/debian-policy/ch-controlfields.html#package") {
				t.Fatalf("expected name validation with policy link, got %v", err)
			}
			if strings.ContainsAny(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") && !strings.Contains(err.Error(), "uppercase letters in the name are not allowed") {
				t.Fatalf("missing uppercase explanation: %v", err)
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("invalid init created output or stat failed: %v", err)
			}
		})
	}
}

func TestInitPackageAcceptsDebianNames(t *testing.T) {
	for _, name := range []string{"my-new-apt-package", "ab", "0a", "libexample1.2+extra"} {
		t.Run(name, func(t *testing.T) {
			cfg := Config{OutDir: filepath.Join(t.TempDir(), "package"), Control: Control{
				Name: name, Version: "1.0", Architecture: "all", Maintainer: "Example", Description: "Fixture",
			}}
			if err := InitPackage(cfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInitPackageLeavesPayloadDirectoriesToCaller(t *testing.T) {
	for _, existingPayload := range []bool{false, true} {
		name := "fresh"
		if existingPayload {
			name = "existing-payload"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "package")
			payload := filepath.Join(root, "usr", "share", "fixture", "data")
			if existingPayload {
				if err := os.MkdirAll(filepath.Dir(payload), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(payload, []byte("preserve me"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := Config{
				OutDir: root,
				Control: Control{
					Name: "fixture", Version: "1.0", Architecture: "all",
					Maintainer: "Example", Description: "Fixture",
				},
			}
			if err := InitPackage(cfg); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"control", "preinst", "postinst", "prerm", "postrm"} {
				info, err := os.Stat(filepath.Join(root, "DEBIAN", name))
				if err != nil {
					t.Fatal(err)
				}
				want := os.FileMode(0755)
				if name == "control" {
					want = 0644
				}
				if !info.Mode().IsRegular() || info.Mode().Perm() != want {
					t.Errorf("%s mode = %v, want regular file with permissions %v", name, info.Mode(), want)
				}
			}
			if existingPayload {
				data, err := os.ReadFile(payload)
				if err != nil || string(data) != "preserve me" {
					t.Fatalf("existing payload changed: %q, %v", data, err)
				}
				if _, err := os.Lstat(filepath.Join(root, "usr", "local")); !os.IsNotExist(err) {
					t.Fatalf("init created usr/local or stat failed: %v", err)
				}
			} else {
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 1 || entries[0].Name() != "DEBIAN" {
					t.Fatalf("init created unexpected payload entries: %v", entries)
				}
			}
		})
	}
}
