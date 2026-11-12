package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lambdawalker/go.apexfission.aws.auth/assetlinks"
)

func writeAssociationFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func publicAssociationFixture() string {
	return `[{"packageName":"com.example.app","sha256CertFingerprints":["` + strings.Repeat("AA:", 31) + `AA"]}]`
}

func TestDiscoverAssociationFilesOnlyListsPublicExports(t *testing.T) {
	home := filepath.Join(t.TempDir(), "user [with spaces]")
	first := filepath.Join(home, ".projects", "a.android", "secrets", "setup-111.vault.associations.json")
	second := filepath.Join(home, ".projects", "z.android", "secrets", "custom.enc.associations.json")
	// Discovery lists paths without parsing or importing any content.
	writeAssociationFixture(t, second, "invalid until selected")
	writeAssociationFixture(t, first, publicAssociationFixture())
	for _, name := range []string{"setup-111.vault", "key.jks", "other.json", "nested/hidden.associations.json"} {
		writeAssociationFixture(t, filepath.Join(filepath.Dir(first), name), "private or unrelated")
	}
	writeAssociationFixture(t, filepath.Join(home, ".projects", "not-a-project"), "file")
	writeAssociationFixture(t, filepath.Join(home, ".attestra", "associations", "qa", "developer-associations.json"), publicAssociationFixture())
	paths, err := discoverAssociationFiles(home)
	if err != nil || !reflect.DeepEqual(paths, []string{first, second}) {
		t.Fatalf("paths=%v error=%v", paths, err)
	}
	apps, err := readAssociationFile(paths[0])
	if err != nil || len(apps) != 1 || apps[0].PackageName != "com.example.app" {
		t.Fatalf("selected file: %v %v", apps, err)
	}
}

func TestDiscoverAssociationFilesMissingRootIsEmpty(t *testing.T) {
	paths, err := discoverAssociationFiles(t.TempDir())
	if err != nil || len(paths) != 0 {
		t.Fatalf("%v %v", paths, err)
	}
}

func TestDiscoverAssociationFilesSkipsSymlinksAndNonregularEntries(t *testing.T) {
	home := t.TempDir()
	secrets := filepath.Join(home, ".projects", "app", "secrets")
	valid := filepath.Join(secrets, "setup-1.vault.associations.json")
	writeAssociationFixture(t, valid, publicAssociationFixture())
	if err := os.Mkdir(filepath.Join(secrets, "directory.associations.json"), 0700); err != nil {
		t.Fatal(err)
	}
	links := [][2]string{
		{valid, filepath.Join(secrets, "link.associations.json")},
		{filepath.Dir(secrets), filepath.Join(home, ".projects", "linked-project")},
		{secrets, filepath.Join(home, ".projects", "other", "secrets")},
	}
	for _, link := range links {
		if err := os.MkdirAll(filepath.Dir(link[1]), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(link[0], link[1]); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
	}
	paths, err := discoverAssociationFiles(home)
	if err != nil || !reflect.DeepEqual(paths, []string{valid}) {
		t.Fatalf("%v %v", paths, err)
	}
	if _, err := readAssociationFile(links[0][1]); err == nil {
		t.Fatal("read a symlink export")
	}
}

func TestReadAssociationFileValidatesPublicSchemaAndSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	valid := publicAssociationFixture()
	for name, content := range map[string]string{
		"object": `{}`, "null": `null`, "empty": `[]`, "malformed": `[`,
		"invalid package":     strings.Replace(valid, "com.example.app", "invalid", 1),
		"invalid fingerprint": `[{"packageName":"com.example.app","sha256CertFingerprints":["not-a-certificate"]}]`,
		"unknown data":        strings.Replace(valid, `"packageName"`, `"privateKey":"must not be imported","packageName"`, 1),
		"trailing JSON":       valid + `{}`, "oversized": valid + strings.Repeat(" ", 1<<20),
	} {
		t.Run(name, func(t *testing.T) {
			writeAssociationFixture(t, path, content)
			if _, err := readAssociationFile(path); err == nil {
				t.Fatal("accepted invalid association input")
			}
		})
	}
	writeAssociationFixture(t, path, valid+"\n")
	apps, err := readAssociationFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []assetlinks.Application{{PackageName: "com.example.app", Fingerprints: []string{strings.Repeat("AA:", 31) + "AA"}}}
	if !reflect.DeepEqual(apps, want) {
		t.Fatalf("%v", apps)
	}
	if _, err := readAssociationFile(filepath.Dir(path)); err == nil {
		t.Fatal("accepted directory")
	}
	if _, err := readAssociationFile(path + ".missing"); err == nil {
		t.Fatal("accepted missing file")
	}
}

func TestCLIExportMergesIntoBackendApprovalsWithoutRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.vault.associations.json")
	writeAssociationFixture(t, path, publicAssociationFixture())
	incoming, err := readAssociationFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Repeat("BB:", 31) + "BB"
	existing := []assetlinks.Application{{PackageName: "com.example.app", Fingerprints: []string{old}}}
	merged, changed, err := mergeAssociations(existing, incoming)
	if err != nil || !changed || len(merged[0].Fingerprints) != 2 || len(existing[0].Fingerprints) != 1 {
		t.Fatalf("%v %v %v", merged, changed, err)
	}
	content, err := assetlinks.Render(merged)
	if err != nil {
		t.Fatal(err)
	}
	var statements []assetlinks.Statement
	if err := json.Unmarshal([]byte(content), &statements); err != nil {
		t.Fatal(err)
	}
	if statements[0].Target.PackageName != "com.example.app" || len(statements[0].Target.Fingerprints) != 2 {
		t.Fatal(content)
	}
}

func TestAssociationSkipCanBeRemembered(t *testing.T) {
	w := &bootstrapWizard{root: t.TempDir(), environment: "qa"}
	if err := w.remember("androidAssociationsSkipped", "true"); err != nil {
		t.Fatal(err)
	}
	saved, err := readBootstrapCheckpoint(w.root, w.environment)
	if err != nil || saved.Settings["androidAssociationsSkipped"] != "true" {
		t.Fatalf("%v %v", saved, err)
	}
}

func TestForcedSetupCanImportCustomExportAfterSkipping(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	w := &bootstrapWizard{root: root, environment: "qa"}
	if err := w.remember("androidAssociationsSkipped", "true"); err != nil {
		t.Fatal(err)
	}
	saved, err := readBootstrapCheckpoint(root, "qa")
	if err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(t.TempDir(), "release.enc.associations.json")
	writeAssociationFixture(t, custom, publicAssociationFixture())
	for _, approve := range []bool{false, true} {
		t.Run(map[bool]string{false: "declined", true: "approved"}[approve], func(t *testing.T) {
			resumed := &bootstrapWizard{root: root, environment: "qa", memory: saved, forceSetup: true}
			defaults := map[string]string{}
			selected, confirmed := false, false
			err := resumed.associationDefaults("", defaults, func(gotHome, environment string, paths []string) (string, error) {
				selected = true
				if gotHome != home || environment != "qa" || len(paths) != 0 {
					t.Fatalf("unexpected import context: %s %s %v", gotHome, environment, paths)
				}
				return custom, nil
			}, func(environment string) (bool, error) {
				confirmed = true
				if environment != "qa" {
					t.Fatalf("wrong approval environment: %s", environment)
				}
				return approve, nil
			})
			if err != nil || !selected || !confirmed {
				t.Fatalf("custom import unreachable: selected=%t confirmed=%t error=%v", selected, confirmed, err)
			}
			if got := defaults["attestra-application:androidAssociations"]; (got != "") != approve || approve && got != publicAssociationFixture() {
				t.Fatalf("approval=%t defaults=%v", approve, defaults)
			}
		})
	}
}
