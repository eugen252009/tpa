package aptpackage

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eugen252009/tpa/internal/version"
)

func TestExtensibleControlMetadataBuildsRealDeb(t *testing.T) {
	root := t.TempDir()
	definition := `{
		"control": {
			"name": "example-backup",
			"version": "1.0.0",
			"architecture": "all",
			"maintainer": "Example <example@example.invalid>",
			"description": "Example backup",
			"packageType": "backup",
			"memaService": "tparun",
			"memaSchema": 11,
			"memaRecovery": true,
			"futureTotallyUnknownField": "works"
		},
		"scripts": {"postinst": "echo installed\n"},
		"outdir": "` + root + `"
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(definition), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := InitPackage(cfg); err != nil {
		t.Fatal(err)
	}

	controlPath := filepath.Join(root, "DEBIAN", "control")
	control, err := os.ReadFile(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(control)
	for _, field := range []string{
		"Package-Type: backup\n",
		"Mema-Recovery: true\n",
		"Mema-Schema: 11\n",
		"Mema-Service: tparun\n",
		"Future-Totally-Unknown-Field: works\n",
		"TPA-Version: " + version.Version + "\n",
	} {
		if !strings.Contains(text, field) {
			t.Errorf("control file missing %q:\n%s", field, text)
		}
	}
	if strings.Contains(text, "Created-With:") {
		t.Fatalf("deprecated Created-With field was generated:\n%s", text)
	}
	if strings.Count(text, "TPA-Version:") != 1 || strings.Count(text, "Created-At:") != 1 {
		t.Fatalf("expected exactly one provenance field of each type:\n%s", text)
	}
	if strings.Contains(text, "Postinst:") || strings.Contains(text, "Postinstbody:") {
		t.Fatalf("script was emitted as control metadata:\n%s", text)
	}
	if _, err := time.Parse(time.RFC3339, fieldValue(text, "Created-At")); err != nil {
		t.Fatalf("Created-At is not RFC3339: %v", err)
	}

	script, err := os.ReadFile(filepath.Join(root, "DEBIAN", "postinst"))
	if err != nil {
		t.Fatal(err)
	}
	if string(script) != "echo installed\n" {
		t.Fatalf("unexpected postinst: %q", script)
	}

	deb := filepath.Join(t.TempDir(), "example-backup_1.0.0_all.deb")
	cmd := exec.Command("dpkg-deb", "--build", "--root-owner-group", root, deb)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v: %s", err, output)
	}
	for _, field := range []string{"Package-Type", "Mema-Schema", "Mema-Recovery", "Mema-Service", "Future-Totally-Unknown-Field", "TPA-Version", "Created-At"} {
		output, err := exec.Command("dpkg-deb", "-f", deb, field).Output()
		if err != nil || strings.TrimSpace(string(output)) == "" {
			t.Fatalf("dpkg-deb did not expose %s: %v %q", field, err, output)
		}
	}
}

func TestNoProvenancePreservesExplicitMetadataInRealDeb(t *testing.T) {
	root := t.TempDir()
	disabled := false
	cfg := Config{
		Control: Control{
			Name: "no-provenance", Version: "1.0", Architecture: "all",
			Maintainer: "Example", Description: "No provenance",
			Metadata: map[string]any{
				"createdAt":                 "2026-01-01T00:00:00Z",
				"tpaVersion":                "custom-test-version",
				"memaType":                  "backup",
				"futureTotallyUnknownField": "works",
			},
		},
		Provenance: &disabled,
		OutDir:     root,
	}
	if err := InitPackage(cfg); err != nil {
		t.Fatal(err)
	}
	control, err := os.ReadFile(filepath.Join(root, "DEBIAN", "control"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(control)
	for _, field := range []string{
		"Created-At: 2026-01-01T00:00:00Z\n",
		"TPA-Version: custom-test-version\n",
		"Mema-Type: backup\n",
		"Future-Totally-Unknown-Field: works\n",
	} {
		if !strings.Contains(text, field) {
			t.Errorf("control file missing explicit/custom field %q:\n%s", field, text)
		}
	}
	if strings.Count(text, "TPA-Version:") != 1 || strings.Count(text, "Created-At:") != 1 {
		t.Fatalf("explicit provenance fields were not preserved exactly once:\n%s", text)
	}

	deb := filepath.Join(t.TempDir(), "no-provenance_1.0_all.deb")
	cmd := exec.Command("dpkg-deb", "--build", "--root-owner-group", root, deb)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v: %s", err, output)
	}
	for field, want := range map[string]string{
		"Created-At":                   "2026-01-01T00:00:00Z",
		"TPA-Version":                  "custom-test-version",
		"Mema-Type":                    "backup",
		"Future-Totally-Unknown-Field": "works",
	} {
		output, err := exec.Command("dpkg-deb", "-f", deb, field).Output()
		if err != nil || strings.TrimSpace(string(output)) != want {
			t.Fatalf("dpkg-deb %s = %q, want %q: %v", field, output, want, err)
		}
	}

	bareRoot := t.TempDir()
	bareCfg := cfg
	bareCfg.OutDir = bareRoot
	bareCfg.Control.Metadata = nil
	if err := InitPackage(bareCfg); err != nil {
		t.Fatal(err)
	}
	bareDeb := filepath.Join(t.TempDir(), "bare-no-provenance_1.0_all.deb")
	cmd = exec.Command("dpkg-deb", "--build", "--root-owner-group", bareRoot, bareDeb)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb bare package: %v: %s", err, output)
	}
	for _, field := range []string{"TPA-Version", "Created-At"} {
		if output, err := exec.Command("dpkg-deb", "-f", bareDeb, field).CombinedOutput(); err != nil {
			continue
		} else if strings.TrimSpace(string(output)) != "" {
			t.Fatalf("dpkg-deb unexpectedly exposed automatic %s: %q", field, output)
		}
	}
}

func TestExplicitProvenanceValuesWin(t *testing.T) {
	control, err := (Control{
		Name: "example", Version: "1.0", Architecture: "all",
		Maintainer: "Example", Description: "Example",
		Metadata: map[string]any{"tpaVersion": "custom", "createdAt": "custom-time"},
	}).RenderWithProvenance(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if fieldValue(control, "TPA-Version") != "custom" || fieldValue(control, "Created-At") != "custom-time" {
		t.Fatalf("explicit provenance values did not win:\n%s", control)
	}
	if strings.Count(control, "TPA-Version:") != 1 || strings.Count(control, "Created-At:") != 1 {
		t.Fatalf("explicit provenance produced duplicates:\n%s", control)
	}
}

func TestParsedCustomMetadataIsExposed(t *testing.T) {
	control, err := ParseControl([]byte("Package: example\nVersion: 1.0\nArchitecture: all\nMaintainer: Example\nDescription: Example\nPackage-Type: backup\nMema-Schema: 11\n"))
	if err != nil {
		t.Fatal(err)
	}
	if control.Metadata["Package-Type"] != "backup" || control.Metadata["Mema-Schema"] != "11" {
		t.Fatalf("parsed custom metadata missing: %+v", control.Metadata)
	}
}

func TestGeneratedProvenanceIsInjected(t *testing.T) {
	control, err := (Control{
		Name: "example", Version: "1.0", Architecture: "all",
		Maintainer: "Example", Description: "Example",
	}).RenderWithProvenance(time.Date(2026, 9, 24, 15, 30, 0, 0, time.FixedZone("test", 3600)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(control, "TPA-Version: "+version.Version+"\n") {
		t.Fatalf("missing generated TPA-Version:\n%s", control)
	}
	if strings.Contains(control, "Created-With:") {
		t.Fatalf("deprecated Created-With was generated:\n%s", control)
	}
	if strings.Count(control, "TPA-Version:") != 1 || strings.Count(control, "Created-At:") != 1 {
		t.Fatalf("duplicate provenance fields:\n%s", control)
	}
	if got := fieldValue(control, "Created-At"); got != "2026-09-24T14:30:00Z" {
		t.Fatalf("Created-At = %q", got)
	}
}

func TestInitPackageUsesSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1767225600")
	root := t.TempDir()
	cfg := Config{
		Control: Control{Name: "reproducible", Version: "1.0", Architecture: "all", Maintainer: "Example", Description: "Reproducible"},
		OutDir:  root,
	}
	if err := InitPackage(cfg); err != nil {
		t.Fatal(err)
	}
	control, err := os.ReadFile(filepath.Join(root, "DEBIAN", "control"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fieldValue(string(control), "Created-At"), "2026-01-01T00:00:00Z"; got != want {
		t.Fatalf("Created-At = %q, want SOURCE_DATE_EPOCH value %q", got, want)
	}
}

func TestInitPackageRejectsInvalidSourceDateEpoch(t *testing.T) {
	for _, value := range []string{"invalid", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", value)
			cfg := Config{
				Control: Control{Name: "invalid-time", Version: "1.0", Architecture: "all", Maintainer: "Example", Description: "Invalid time"},
				OutDir:  t.TempDir(),
			}
			if err := InitPackage(cfg); err == nil {
				t.Fatal("InitPackage accepted invalid SOURCE_DATE_EPOCH")
			}
		})
	}
}

func TestAllMaintainerScriptsAreWrittenSeparately(t *testing.T) {
	root := t.TempDir()
	cfg := Config{
		Control: Control{Name: "scripts", Version: "1.0", Architecture: "all", Maintainer: "Example", Description: "Scripts"},
		Scripts: MaintainerScripts{PreInst: "pre\n", PostInst: "post\n", PreRm: "prerm\n", PostRm: "postrm\n"},
		OutDir:  root,
	}
	if err := InitPackage(cfg); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"preinst": "pre\n", "postinst": "post\n", "prerm": "prerm\n", "postrm": "postrm\n"} {
		data, err := os.ReadFile(filepath.Join(root, "DEBIAN", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", name, data, want)
		}
	}
	control, err := os.ReadFile(filepath.Join(root, "DEBIAN", "control"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"Preinst:", "Postinst:", "Prerm:", "Postrm:", "Preinstbody:", "Postinstbody:", "Prmbody:", "Postrmbody:"} {
		if strings.Contains(string(control), field) {
			t.Errorf("control contains script field %q:\n%s", field, control)
		}
	}
}

func TestLegacyScriptFieldsNormalizeToScripts(t *testing.T) {
	var cfg Config
	input := `{"control":{"name":"legacy","version":"1.0","architecture":"all","maintainer":"Example","description":"Legacy","preinstbody":"echo pre\n","postrmbody":"echo rm\n"},"outdir":"/tmp/legacy"}`
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Scripts.PreInst != "echo pre\n" || cfg.Scripts.PostRm != "echo rm\n" {
		t.Fatalf("legacy scripts were not normalized: %+v", cfg.Scripts)
	}
	if len(cfg.Control.Metadata) != 0 {
		t.Fatalf("legacy script fields entered metadata: %+v", cfg.Control.Metadata)
	}
}

func TestControlJSONRoundTripPreservesCustomMetadata(t *testing.T) {
	disabled := false
	original := Config{Control: Control{
		Name: "roundtrip", Version: "1.0", Architecture: "all",
		Maintainer: "Example", Description: "Round trip",
		Metadata: map[string]any{"packageType": "backup", "memaSchema": json.Number("11")},
	}, Scripts: MaintainerScripts{PostInst: "echo ok\n"}, Provenance: &disabled}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Control.Metadata["packageType"] != "backup" || restored.Control.Metadata["memaSchema"] != json.Number("11") {
		t.Fatalf("custom metadata was not preserved: %+v", restored.Control.Metadata)
	}
	if restored.Scripts.PostInst != "echo ok\n" {
		t.Fatalf("scripts were not preserved: %+v", restored.Scripts)
	}
	if restored.Provenance == nil || *restored.Provenance {
		t.Fatalf("provenance setting was not preserved: %v", restored.Provenance)
	}
}

func TestInvalidCustomMetadataIsRejected(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"empty key", `{"control":{"":"value"}}`},
		{"colon key", `{"control":{"bad:key":"value"}}`},
		{"newline key", `{"control":{"bad\nkey":"value"}}`},
		{"newline value", `{"control":{"packageType":"safe\nInjected: yes"}}`},
		{"array value", `{"control":{"packageType":["backup"]}}`},
		{"object value", `{"control":{"packageType":{"kind":"backup"}}}`},
		{"standard collision", `{"control":{"package":"override"}}`},
		{"normalized collision", `{"control":{"packageType":"one","package-type":"two"}}`},
		{"provenance collision", `{"control":{"tpaVersion":"one","TPA-Version":"two"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var cfg Config
			if err := json.Unmarshal([]byte(test.json), &cfg); err == nil {
				t.Fatal("expected metadata validation error")
			}
		})
	}
}

func fieldValue(control, field string) string {
	prefix := field + ": "
	for _, line := range strings.Split(control, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}
