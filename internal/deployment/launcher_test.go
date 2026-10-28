package deployment

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellLauncherUsesRepositoryAndPreservesArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	root := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../deploy.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "deploy.sh"), script, 0700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err = os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\npwd\nprintf '<%s>\\n' \"$@\"\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(root, "deploy.sh"), "--prefix", "", "--admin-email", "person with space@example.com")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("launcher hid failure")
	}
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 7 {
		t.Fatal(err)
	}
	want := root + "\n<run>\n<-buildvcs=false>\n<./cmd/deploy>\n<--prefix>\n<>\n<--admin-email>\n<person with space@example.com>\n"
	if strings.ReplaceAll(string(out), "\r\n", "\n") != want {
		t.Fatalf("arguments/cwd changed: %q", out)
	}
}
