package adminapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/lambdawalker/go.attestra.aws/artifactsnapshot"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

// SnapshotReader exposes no writer capability to the dashboard handler.
type SnapshotReader interface {
	Read(context.Context, string) (reporting.Report, error)
}

func (h Handler) artifacts(ctx context.Context, env string, html bool) events.APIGatewayV2HTTPResponse {
	if h.Artifacts == nil {
		return reply(503, map[string]string{"error": "snapshot_unavailable"})
	}
	r, e := h.Artifacts.Read(ctx, env)
	if e != nil {
		code, status := "snapshot_unavailable", 503
		switch {
		case errors.Is(e, artifactsnapshot.ErrMissing):
			code, status = "missing_snapshot", 404
		case errors.Is(e, artifactsnapshot.ErrCorrupt):
			code = "corrupt_snapshot"
		}
		return reply(status, map[string]string{"error": code})
	}
	if !html {
		return reply(200, r)
	}
	body, e := artifactsnapshot.HTML(r)
	if errors.Is(e, artifactsnapshot.ErrRenderTooLarge) {
		return reply(413, map[string]string{"error": "report_too_large"})
	}
	if e != nil {
		return reply(503, map[string]string{"error": "snapshot_unavailable"})
	}
	return events.APIGatewayV2HTTPResponse{StatusCode: 200, Headers: map[string]string{"content-type": "text/html; charset=utf-8", "x-artifact-finished-at": r.Metadata.FinishedAt.Format("2006-01-02T15:04:05.999999999Z07:00"), "cache-control": "no-store", "x-content-type-options": "nosniff", "content-security-policy": reportCSP, "referrer-policy": "no-referrer"}, Body: body}
}

// srcdoc/blob inherit their creator's CSP. Hash only the exact static blocks in
// the pinned trusted Composer renderer; never permit arbitrary inline scripts.
var reportHashes = func() map[string]string {
	var b bytes.Buffer
	if reporting.WriteHTML(&b, reporting.Report{}) != nil {
		return nil
	}
	out := map[string]string{}
	for kind, pattern := range map[string]string{"style": `(?s)<style>(.*?)</style>`, "script": `(?s)<script[^>]*>(.*?)</script>`} {
		var hashes []string
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(b.String(), -1) {
			sum := sha256.Sum256([]byte(m[1]))
			hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}
		out[kind] = strings.Join(hashes, " ")
	}
	return out
}()
var reportCSP = "default-src 'none'; script-src " + reportHashes["script"] + "; style-src " + reportHashes["style"] + "; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
