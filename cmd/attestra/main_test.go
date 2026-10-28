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
	for _, op := range []string{"check", "preview", "deploy", "teardown"} {
		e := run(context.Background(), []string{op, "--manifest", manifest}, &out)
		if e == nil || !strings.Contains(e.Error(), "real AWS account") {
			t.Fatal(op, e)
		}
	}
}
