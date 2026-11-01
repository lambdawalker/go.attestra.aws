package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompositionCommandIsLocalAndProductionRejectsFixture(t *testing.T) {
	var out, errout bytes.Buffer
	if code := run([]string{"-json"}, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "authentication.backend") {
		t.Fatal(out.String())
	}
	out.Reset()
	errout.Reset()
	if code := run([]string{"-environment", "prod"}, &out, &errout); code != 1 || !strings.Contains(errout.String(), "IC_TEST_PROVIDER") {
		t.Fatal(code, errout.String())
	}
}
