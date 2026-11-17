package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestContentCacheAndTamperRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Go executable; cache verification is platform independent")
	}
	root := t.TempDir()
	bin := t.TempDir()
	source := filepath.Join(root, "main.go")
	version := filepath.Join(root, "toolchain")
	for name, value := range map[string]string{"main.go": "package main", "go.mod": "module example", "go.sum": "sum", "toolchain": "go1.test"} {
		os.WriteFile(filepath.Join(root, name), []byte(value), 0600)
	}
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n version) cat '%s';;\n env) echo '{}';;\n list) printf '%%s' '{\"Dir\":\"%s\",\"GoFiles\":[\"main.go\"]}';;\n *) exit 1;;\nesac\n", version, filepath.ToSlash(root))
	os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	job := lambdaBuild{source: "./cmd/test", names: []string{"one", "two"}}
	builds := 0
	run := func() error {
		return withBuildLock(root, func() error {
			return cachedBuild(context.Background(), root, job, func(binary string) error { builds++; return os.WriteFile(binary, []byte("binary"), 0700) })
		})
	}
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if builds != 1 {
		t.Fatal("unchanged rerun rebuilt")
	}
	os.WriteFile(source, []byte("changed source"), 0600)
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if builds != 2 {
		t.Fatal("source change ignored")
	}
	os.WriteFile(filepath.Join(root, "dist", "two.zip"), []byte("tampered"), 0600)
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if builds != 3 {
		t.Fatal("tampered archive reused")
	}
	os.WriteFile(version, []byte("new toolchain"), 0600)
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if builds != 4 {
		t.Fatal("toolchain change ignored")
	}
	os.Remove(filepath.Join(root, "dist", "one.zip"))
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if builds != 5 {
		t.Fatal("missing artifact reused")
	}
	if e := withBuildLock(root, func() error {
		if withBuildLock(root, func() error { t.Fatal("concurrent writer entered"); return nil }) == nil {
			t.Fatal("lock ignored")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
