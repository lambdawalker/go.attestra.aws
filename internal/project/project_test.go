package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitAndLoadLinkedModules(t *testing.T) {
	p := filepath.Join(t.TempDir(), "apexfission.json")
	if e := Init(p); e != nil {
		t.Fatal(e)
	}
	m, e := Load(p)
	if e != nil || len(m.Builds) != 2 || m.Application != "attestra" {
		t.Fatal(m, e)
	}
	before, _ := os.ReadFile(p)
	if e = Init(p); e == nil {
		t.Fatal("overwrote configuration")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("changed existing config")
	}
}
