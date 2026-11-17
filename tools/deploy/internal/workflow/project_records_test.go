package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRecordsRejectCollisionsAndConcurrentOperators(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p := projectIdentity{1, "example", "owner/repo", t.TempDir()}
	if e := validateCentralProject(p); e != nil {
		t.Fatal(e)
	}
	if e := validateCentralProject(p); e != nil {
		t.Fatal(e)
	}
	other := p
	other.Repository = "other/repo"
	if validateCentralProject(other) == nil {
		t.Fatal("project collision accepted")
	}
	other = p
	other.Checkout = t.TempDir()
	if validateCentralProject(other) == nil {
		t.Fatal("checkout collision accepted")
	}
	root := p.Checkout
	if e := atomicPrivateJSON(projectPath(root), p); e != nil {
		t.Fatal(e)
	}
	if stateDirectory(root) != filepath.Join(home, ".infra-composer", "example") {
		t.Fatal("noncanonical record path")
	}
	release, e := acquireOperatorLock(root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = acquireOperatorLock(root); e == nil {
		t.Fatal("concurrent operator accepted")
	}
	release()
	release, e = acquireOperatorLock(root)
	if e != nil {
		t.Fatal(e)
	}
	release()
	os.WriteFile(projectPath(root), []byte("corrupt"), 0600)
	if _, e = readProject(root); e == nil {
		t.Fatal("corrupt identity accepted")
	}
}
func TestRecordMigrationIsExplicitAndRetainsLegacyCopies(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, ".attestra")
	os.Mkdir(old, 0700)
	path := filepath.Join(old, "bootstrap.dev.local.json")
	os.WriteFile(path, []byte(`{"Repository":"owner/repo","Stack":"dev"}`), 0600)
	if prepareGeneratedState(root) == nil {
		t.Fatal("implicit migration allowed")
	}
	if e := migrateGeneratedState(root); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); e != nil {
		t.Fatal("recovery copy removed")
	}
	if e := prepareGeneratedState(root); e != nil {
		t.Fatal(e)
	}
}
