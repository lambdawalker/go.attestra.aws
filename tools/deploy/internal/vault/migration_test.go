package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitMigrationRetainsRecoveryAndScope(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "legacy.enc")
	phrase := "nebula violin orchard cobalt lantern 839"
	v, e := Create(old, "old-repository-environment", phrase)
	if e != nil {
		t.Fatal(e)
	}
	values := Credentials{Pulumi: "existing-pulumi", IndexPulumi: "existing-shared", Cloudflare: "existing-token"}
	if e = v.Save(values); e != nil {
		t.Fatal(e)
	}
	v.Close()
	before, _ := os.ReadFile(old)
	target, scope, e := LocalLocation(root, "project-a", "owner/repo", "dev")
	if e != nil {
		t.Fatal(e)
	}
	if Migrate(old, "wrong-scope", target, scope, phrase) == nil {
		t.Fatal("wrong legacy scope accepted")
	}
	if Migrate(old, "old-repository-environment", target, scope, "incorrect phrase") == nil {
		t.Fatal("wrong phrase accepted")
	}
	if e = Migrate(old, "old-repository-environment", target, scope, phrase); e != nil {
		t.Fatal(e)
	}
	next, actual, e := Unlock(target, scope, phrase)
	if e != nil || actual != values {
		t.Fatal("secrets changed", e)
	}
	next.Close()
	if _, _, e = Unlock(target, "renamed-scope", phrase); e == nil {
		t.Fatal("scope bypassed")
	}
	after, _ := os.ReadFile(old)
	if !bytes.Equal(before, after) {
		t.Fatal("recovery vault changed")
	}
	if Migrate(old, "old-repository-environment", target, scope, phrase) == nil {
		t.Fatal("existing destination overwritten")
	}
}
