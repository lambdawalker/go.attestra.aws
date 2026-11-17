package workflow

import (
	"strings"
	"testing"
)

func TestCIModes(t *testing.T) {
	for _, mode := range []string{"preview", "deploy"} {
		o := testOptions()
		o.CI = mode
		o.ExpectedAccount = "123456789012"
		o.Approved = true
		c := credentials{Access: "id", Secret: "secret", Token: "session", Passphrase: "pass"}
		if err := validateCI(o, c); err != nil {
			t.Fatal(err)
		}
		r := goodRunner()
		if err := execute(r, o); err != nil {
			t.Fatal(err)
		}
		want := 6
		if mode == "deploy" {
			want = 7
		}
		if len(r.calls) != want || r.calls[5] != "pulumi preview --stack dev --non-interactive" {
			t.Fatal(r.calls)
		}
		if mode == "deploy" && r.calls[6] != "pulumi up --stack dev --yes --non-interactive" {
			t.Fatal(r.calls)
		}
		r = goodRunner()
		r.fail = 6
		if execute(r, o) == nil || len(r.calls) != 6 {
			t.Fatal("deployed after failed CI preview")
		}
		for _, mutate := range []func(*options){func(o *options) { o.CI = "typo" }, func(o *options) { o.Sso = true }, func(o *options) { o.Login = true }, func(o *options) { o.Pull = true }, func(o *options) { o.MigrateFrom = "isdavid/attestra-application/dev" }} {
			changed := o
			mutate(&changed)
			if validateCI(changed, c) == nil {
				t.Fatal("unsafe CI options accepted")
			}
		}
		c.Passphrase = ""
		if validateCI(o, c) == nil {
			t.Fatal("missing passphrase accepted")
		}
	}
	// Interactive execution still has no auto-approval.
	r := goodRunner()
	if err := execute(r, testOptions()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(r.calls, "\n"), "--yes") {
		t.Fatal("interactive deployment auto-approved")
	}
}
