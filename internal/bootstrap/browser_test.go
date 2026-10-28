package bootstrap

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	"github.com/lambdawalker/go.apexfission.aws.auth/authorization"
	frameworkconfig "github.com/lambdawalker/go.apexfission.aws.auth/config"
	"github.com/lambdawalker/go.apexfission.aws.auth/router"
	"github.com/lambdawalker/go.attestra.aws/app"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedLoginCSPAllowsOnlyConfiguredTokenOrigin(t *testing.T) {
	s := Settings{Mode: "admin", Environment: "dev", Issuer: "https://cognito-idp.us-east-1.amazonaws.com/admin-pool", ClientID: "admin-client", ConfigTable: "config", LoginDomain: "https://login.auth.us-east-1.amazoncognito.com", AdminOrigin: "https://admin.example"}
	h, e := Handler(context.Background(), aws.Config{Region: "us-east-1"}, s)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://admin.example/admin", nil))
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "connect-src 'self' "+s.LoginDomain) || strings.Contains(policy, "connect-src *") {
		t.Fatalf("managed login blocked or overly broad CSP: %s", policy)
	}
}

func TestBrowserSessionCanCallAdminExtension(t *testing.T) {
	a, e := app.Compose(auth.Module{})
	if e != nil {
		t.Fatal(e)
	}
	shell, e := a.Admin(router.VerifierFunc(func(_ context.Context, token string) (authorization.Principal, error) {
		if token != "valid" {
			return authorization.Principal{}, fmt.Errorf("invalid")
		}
		return authorization.Principal{Subject: "admin"}, nil
	}), authorization.AuthorizerFunc(func(context.Context, authorization.Principal, string, string) error { return nil }), []string{"dev"}, frameworkconfig.NewMemory())
	if e != nil {
		t.Fatal(e)
	}
	h, e := managedLoginBrowser(shell, "https://login.example")
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "https://admin.example/admin/api/extensions/attestra/status?env=dev", nil)
	r.Header.Set("Cookie", "__Host-apex-session=valid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "https://admin.example/admin/api/extensions/attestra/status?env=dev", nil)
	r.Header.Set("Cookie", "__Host-apex-session=valid")
	r.Header.Set("Origin", "https://attacker.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("lost CSRF check", w.Code)
	}
}
