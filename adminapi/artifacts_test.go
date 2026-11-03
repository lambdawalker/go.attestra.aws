package adminapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/lambdawalker/go.attestra.aws/artifactsnapshot"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"regexp"
	"strings"
	"testing"
)

type snapshotFake struct {
	calls int
	r     reporting.Report
	err   error
}

func (f *snapshotFake) Read(context.Context, string) (reporting.Report, error) {
	f.calls++
	return f.r, f.err
}
func TestArtifactsAuthorizationAndReadOnly(t *testing.T) {
	h, _ := setup()
	f := &snapshotFake{}
	h.Artifacts = f
	ctx := context.Background()
	for _, path := range []string{"/api/environments/dev/artifacts", "/api/environments/dev/artifacts-report"} {
		for _, claim := range []string{"iss", "client_id", "token_use", "sub"} {
			r := request(h, "GET", path)
			r.RequestContext.Authorizer.JWT.Claims[claim] = ""
			out, _ := h.Handle(ctx, r)
			if out.StatusCode != 401 || f.calls != 0 {
				t.Fatal(out)
			}
		}
		out, _ := h.Handle(ctx, request(h, "GET", strings.Replace(path, "dev", "prod", 1)))
		if out.StatusCode != 403 || f.calls != 0 {
			t.Fatal(out)
		}
		for _, method := range []string{"POST", "DELETE", "PUT", "PATCH"} {
			out, _ = h.Handle(ctx, request(h, method, path))
			if out.StatusCode == 200 || f.calls != 0 {
				t.Fatal(out)
			}
		}
	}
}
func TestArtifactResponseStatesAndTrustedCSP(t *testing.T) {
	h, _ := setup()
	f := &snapshotFake{}
	h.Artifacts = f
	ctx := context.Background()
	for _, state := range []struct {
		err    error
		status int
		code   string
	}{{artifactsnapshot.ErrMissing, 404, "missing_snapshot"}, {artifactsnapshot.ErrCorrupt, 503, "corrupt_snapshot"}, {errors.New("private details"), 503, "snapshot_unavailable"}} {
		f.err = state.err
		out, _ := h.Handle(ctx, request(h, "GET", "/api/environments/dev/artifacts"))
		if out.StatusCode != state.status || !strings.Contains(out.Body, state.code) || strings.Contains(out.Body, "private") {
			t.Fatal(out)
		}
	}
	f.err = nil
	f.r = reporting.Report{Scope: reporting.Scope{Environment: "dev"}}
	out, _ := h.Handle(ctx, request(h, "GET", "/api/environments/dev/artifacts-report"))
	if out.StatusCode != 200 || out.Headers["cache-control"] != "no-store" || out.Headers["x-content-type-options"] != "nosniff" || !strings.Contains(out.Body, "filter-status") {
		t.Fatal(out)
	}
	csp := asset("text/html", index).Headers["content-security-policy"]
	if strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "frame-src blob:") {
		t.Fatal(csp)
	}
	for _, pattern := range []string{`(?s)<style>(.*?)</style>`, `(?s)<script[^>]*>(.*?)</script>`} {
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(out.Body, -1) {
			sum := sha256.Sum256([]byte(m[1]))
			if !strings.Contains(csp, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
				t.Fatal("report inline content blocked by CSP")
			}
		}
	}
	f.r.Artifacts = nil
	for i := 0; i < 3500; i++ {
		f.r.Artifacts = append(f.r.Artifacts, reporting.Artifact{Key: fmt.Sprintf("artifact-%d", i), Name: "sample", Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}})
	}
	out, _ = h.Handle(ctx, request(h, "GET", "/api/environments/dev/artifacts-report"))
	if out.StatusCode != 413 || !strings.Contains(out.Body, "report_too_large") {
		t.Fatal(out.StatusCode, out.Body[:min(100, len(out.Body))])
	}
	out, _ = h.Handle(ctx, request(h, "GET", "/api/environments/dev/artifacts"))
	if out.StatusCode != 200 {
		t.Fatal(out)
	}
}

func TestEmbeddedDashboardContainsIsolatedExplorer(t *testing.T) {
	if !strings.Contains(index, `sandbox="allow-scripts"`) || strings.Contains(index, "allow-same-origin") || !strings.Contains(index, "artifactsPanel") || !strings.Contains(script, "downloadArtifacts") || !strings.Contains(style, "#artifactFrame") {
		t.Fatal("embedded admin assets omit isolated explorer")
	}
}
