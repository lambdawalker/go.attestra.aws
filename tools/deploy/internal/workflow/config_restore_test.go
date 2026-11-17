package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshCheckoutRestoresBeforeReadingSettings(t *testing.T) {
	o := testOptions()
	o.Root = t.TempDir()
	r := &bootstrapFake{list: `[{"name":"dev"}]`, config: `{"attestra-application:appOrigin":{"value":"https://original.example.com"}}`}
	cfg, e := readAuthoritativeConfiguration(r, o, true)
	if e != nil || cfg["attestra-application:appOrigin"].Value != "https://original.example.com" {
		t.Fatal("original configuration not restored", e)
	}
	calls := strings.Join(r.calls, "\n")
	if strings.Index(calls, "config refresh") > strings.Index(calls, "config --json") {
		t.Fatal("settings read before restoration")
	}
	r = &bootstrapFake{list: `[]`}
	if _, e = readAuthoritativeConfiguration(r, o, true); e == nil {
		t.Fatal("lost initialized stack treated as new")
	}
	r = &bootstrapFake{fail: "stack ls"}
	if _, e = readAuthoritativeConfiguration(r, o, false); e == nil {
		t.Fatal("lookup error treated as absence")
	}
}
func TestRestorationRetainsEncryptedConfigurationCopy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "infra")
	os.Mkdir(dir, 0700)
	path := filepath.Join(dir, "Pulumi.dev.yaml")
	original := "encryptionsalt: existing\nconfig:\n  proofKey:\n    secure: existing-ciphertext\n"
	os.WriteFile(path, []byte(original), 0600)
	args := []string{"config", "refresh", "--stack", "dev", "--force"}
	if e := backupStackConfiguration(root, dir, args); e != nil {
		t.Fatal(e)
	}
	if e := backupStackConfiguration(root, dir, args); e != nil {
		t.Fatal("idempotent backup failed", e)
	}
	files, _ := filepath.Glob(filepath.Join(root, ".deploy", "config-backups", "attestra-application", "*.yaml"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	data, _ := os.ReadFile(files[0])
	if string(data) != original {
		t.Fatal("encrypted recovery data changed")
	}
}
