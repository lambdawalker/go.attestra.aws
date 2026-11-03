package main

import (
	"context"
	"github.com/lambdawalker/go.attestra.aws/artifactsnapshot"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type publisherFake struct{ calls int }

func (f *publisherFake) Publish(context.Context, string, reporting.Report) (artifactsnapshot.PublishOutcome, error) {
	f.calls++
	return artifactsnapshot.Published, nil
}

const report = `{"schemaVersion":1,"scope":{"application":"attestra","environment":"dev","stack":"dev","project":"attestra-application","account":"123456789012","region":"us-east-2","repository":"owner/repo","backend":"s3://state"},"metadata":{"operation":"deploy","outcome":"failed","startedAt":"2026-11-03T00:00:00Z","finishedAt":"2026-11-03T01:00:00Z"},"artifacts":[]}`

func TestPrivateBoundedInputAndTypedPublication(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "report.json")
	f := &publisherFake{}
	os.WriteFile(p, []byte(report), 0600)
	if _, e := publishFile(context.Background(), f, "dev", p); e != nil || f.calls != 1 {
		t.Fatal(e, f.calls)
	}
	for _, b := range []string{`{}`, report + ` {}`, report[:len(report)-1] + `,"rawState":"private"}`} {
		os.WriteFile(p, []byte(b), 0600)
		if _, e := publishFile(context.Background(), f, "dev", p); e == nil {
			t.Fatal("accepted invalid file")
		}
	}
	os.WriteFile(p, []byte(report), 0600)
	if runtime.GOOS != "windows" {
		os.Chmod(p, 0644)
		if _, e := publishFile(context.Background(), f, "dev", p); e == nil {
			t.Fatal("accepted nonprivate input")
		}
	}
	if f.calls != 1 {
		t.Fatal("invalid input reached writer")
	}
}
