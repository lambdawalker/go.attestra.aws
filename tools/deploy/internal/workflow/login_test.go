package workflow

import (
	"errors"
	"strings"
	"testing"
)

type loginStub struct {
	calls    []string
	captured []bool
	fail     int
	response string
}

func (r *loginStub) Exec(_ string, capture bool, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	r.captured = append(r.captured, capture)
	if r.fail == len(r.calls) {
		return nil, errors.New("failed")
	}
	if capture {
		return []byte(r.response), nil
	}
	return nil, nil
}
func TestLoginCredentials(t *testing.T) {
	o := testOptions()
	o.Profile = "attestra"
	good := `{"Version":1,"AccessKeyId":"id","SecretAccessKey":"secret","SessionToken":"token","Expiration":"2099-01-01T00:00:00Z"}`
	r := &loginStub{response: good}
	c, err := loginCredentials(r, o)
	if err != nil || c.Source == nil || c.Source.Profile != "attestra" {
		t.Fatal("login credentials not extracted")
	}
	if len(r.calls) != 2 || r.calls[0] != "aws login --profile attestra --region us-east-2" || r.calls[1] != "aws configure export-credentials --profile attestra --format process" || r.captured[0] || !r.captured[1] {
		t.Fatal(r.calls)
	}
	for i := 1; i <= 2; i++ {
		r := &loginStub{response: good, fail: i}
		if _, err := loginCredentials(r, o); err == nil || len(r.calls) != i {
			t.Fatal("continued after failure")
		}
	}
	for _, bad := range []string{`not JSON`, `{}`, `{"Version":1,"AccessKeyId":"id","SecretAccessKey":"secret"}`} {
		if _, err := loginCredentials(&loginStub{response: bad}, o); err == nil {
			t.Fatal("accepted incomplete credentials")
		}
	}
}
func TestLoginEnvironment(t *testing.T) {
	env := strings.Join(loginEnvironment([]string{"PATH=/bin", "AWS_ACCESS_KEY_ID=stale", "AWS_PROFILE=wrong", "AWS_ENDPOINT_URL=bad", "AWS_CONFIG_FILE=/config", "AWS_SHARED_CREDENTIALS_FILE=/credentials", "AWS_LOGIN_CACHE_DIRECTORY=/cache", "PULUMI_ACCESS_TOKEN=secret"}, "us-east-2"), "\n")
	for _, bad := range []string{"stale", "wrong", "bad", "secret"} {
		if strings.Contains(env, bad) {
			t.Fatal("inherited credential override")
		}
	}
	for _, want := range []string{"AWS_CONFIG_FILE=/config", "AWS_SHARED_CREDENTIALS_FILE=/credentials", "AWS_LOGIN_CACHE_DIRECTORY=/cache"} {
		if !strings.Contains(env, want) {
			t.Fatal("missing profile location")
		}
	}
}

func TestSSOLoginCredentials(t *testing.T) {
	o := testOptions()
	o.Profile = "attestra"
	o.Sso = true
	good := `{"Version":1,"AccessKeyId":"id","SecretAccessKey":"secret","SessionToken":"token","Expiration":"2099-01-01T00:00:00Z"}`
	cached := &loginStub{response: good}
	c, e := loginCredentials(cached, o)
	if e != nil || c.Source == nil || !c.Source.SSO || len(cached.calls) != 1 {
		t.Fatal("valid SSO session not reused", e, cached.calls)
	}
	expired := &loginStub{response: good, fail: 1}
	if _, e := loginCredentials(expired, o); e != nil || len(expired.calls) != 3 || expired.calls[1] != "aws sso login --profile attestra" {
		t.Fatal("expired session not renewed", e, expired.calls)
	}
	failed := &loginStub{fail: 2}
	if _, e := loginCredentials(failed, o); e == nil || len(failed.calls) != 2 {
		t.Fatal("continued after failed SSO login")
	}
}

func TestLoginDoesNotFreezeTemporaryKeys(t *testing.T) {
	r := &loginStub{response: `{"Version":1,"AccessKeyId":"id","SecretAccessKey":"secret","SessionToken":"token","Expiration":"2099-01-01T00:00:00Z"}`}
	o := testOptions()
	o.Profile = "attestra"
	o.Sso = true
	c, e := loginCredentials(r, o)
	if e != nil {
		t.Fatal(e)
	}
	if c.Access != "" || c.Secret != "" || c.Token != "" {
		t.Fatal("login retained a fixed credential snapshot instead of a renewable source")
	}
}
