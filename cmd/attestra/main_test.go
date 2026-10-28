package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaceholdersBlockCloudOperation(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "apexfission.json")
	var out bytes.Buffer
	if e := run(context.Background(), []string{"init", "--manifest", manifest}, &out); e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"check"} {
		e := run(context.Background(), []string{op, "--manifest", manifest}, &out)
		if e == nil || !strings.Contains(e.Error(), "real AWS account") {
			t.Fatal(op, e)
		}
	}
}

func TestCloudCommandsDirectToWizardBeforeReadingManifest(t *testing.T) {
	for _, op := range []string{"preview", "deploy", "status", "teardown"} {
		err := run(context.Background(), []string{op, "--manifest", filepath.Join(t.TempDir(), "missing.json")}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "deploy.sh / deploy.bat") {
			t.Fatal(op, err)
		}
	}
}
