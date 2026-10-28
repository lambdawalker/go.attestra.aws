package app

import (
	"context"
	"encoding/json"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	"github.com/lambdawalker/go.apexfission.aws.auth/authorization"
	"github.com/lambdawalker/go.apexfission.aws.auth/router"
	"net/http/httptest"
	"testing"
)

func TestApplicationRoutes(t *testing.T) {
	a, e := Compose(auth.Module{})
	if e != nil {
		t.Fatal(e)
	}
	v := router.VerifierFunc(func(context.Context, string) (authorization.Principal, error) {
		return authorization.Principal{Subject: "customer-123", Issuer: "https://issuer.example"}, nil
	})
	h, e := a.Handler(v, ProfileAuthorizer{}, "dev")
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		path, token string
		status      int
	}{{"/health", "", 200}, {"/v1/me", "", 401}, {"/v1/me", "valid", 200}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
		if tc.path == "/v1/me" && tc.status == 200 {
			var p struct {
				Subject string `json:"subject"`
			}
			if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Subject != "customer-123" {
				t.Fatal(w.Body.String())
			}
		}
	}
	if len(a.Builds) != 2 || len(a.Pages) != 1 {
		t.Fatal("missing contributions")
	}
}
func TestProfileAuthorizationDoesNotGrantAdministration(t *testing.T) {
	a := ProfileAuthorizer{}
	if a.Authorize(context.Background(), authorization.Principal{Subject: "u"}, "dev", "attestra:admin") == nil {
		t.Fatal("customer became administrator")
	}
	if a.Authorize(context.Background(), authorization.Principal{}, "dev", "attestra:profile") == nil {
		t.Fatal("empty subject accepted")
	}
}
