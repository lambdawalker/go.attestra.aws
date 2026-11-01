package adminapi

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/lambdawalker/go.attestra.aws/internal/awshttp"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lambdawalker/go.apexfission.aws.auth/envreset"
	"github.com/lambdawalker/go.apexfission.aws.auth/runtimeconfig"
)

func TestResetFunctionName(t *testing.T) {
	for _, tt := range []struct{ prefix, environment, want string }{
		{"", "dev", "attestra-app-auth-reset-dev"},
		{ResetFunctionPrefix, "prod", "attestra-app-auth-reset-prod"},
		{"isolated-reset-", "dev", "isolated-reset-dev"},
		{"arn:aws:lambda:us-east-1:123:function:", "dev", ""},
		{"reset-*", "dev", ""}, {"reset/", "dev", ""},
		{"reset", "dev", ""}, {strings.Repeat("x", 32) + "-", "dev", ""},
		{"reset-", "../prod", ""}, {"reset-", "arn:aws:lambda:other", ""},
	} {
		got, err := ResetFunctionName(tt.prefix, tt.environment)
		if got != tt.want || (err != nil) != (tt.want == "") {
			t.Errorf("(%q,%q) = %q, %v", tt.prefix, tt.environment, got, err)
		}
	}
}

func TestResetRejectsInvalidTargetBeforeCredentials(t *testing.T) {
	for _, client := range []LambdaResetClient{{FunctionPrefix: "arn:aws:lambda:"}, {FunctionPrefix: "reset-*"}} {
		_, err := client.Invoke(context.Background(), envreset.Request{Environment: "dev"})
		if !errors.Is(err, runtimeconfig.ErrInvalid) {
			t.Fatalf("invalid target accepted: %v", err)
		}
	}
}

// Prefix and environment resolve on the server; no browser field selects a target.
func TestResetInvocationUsesBoundedServerTarget(t *testing.T) {
	for _, prefix := range []string{"", "isolated-reset-"} {
		want, _ := ResetFunctionName(prefix, "dev")
		called := false
		c := LambdaResetClient{FunctionPrefix: prefix, HTTP: awshttp.Client{
			Config: aws.Config{Region: "us-east-1", Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
			})},
			HTTP: &http.Client{Transport: resetTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.URL.Host != "lambda.us-east-1.amazonaws.com" || r.URL.Path != "/2015-03-31/functions/"+want+"/invocations" {
					t.Fatalf("unexpected target %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":{"environment":"dev","enabled":true}}`))}, nil
			})},
		}}
		if _, err := c.Invoke(context.Background(), envreset.Request{Environment: "dev", Action: "status"}); err != nil || !called {
			t.Fatalf("invocation: %v, called=%v", err, called)
		}
	}
}

type resetTransport func(*http.Request) (*http.Response, error)

func (f resetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
