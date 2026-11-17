package workflow

import (
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
	"strings"
	"testing"
)

func TestTLSPolicyRequiresUniversalBucketAndObjectDeny(t *testing.T) {
	good := tlsStatePolicy("state")
	if !deniesInsecureState(good, "state") {
		t.Fatal("secure policy rejected")
	}
	for _, bad := range []string{strings.ReplaceAll(good, "Deny", "Allow"), strings.ReplaceAll(good, "\"false\"", "\"true\""), strings.ReplaceAll(good, "state/*", "other/*"), strings.ReplaceAll(good, "s3:*", "s3:GetObject"), `invalid`} {
		if deniesInsecureState(bad, "state") {
			t.Fatal("insecure policy accepted")
		}
	}
}
func TestInitializedStateBucketNeverRecreated(t *testing.T) {
	o := testOptions()
	o.ExistingState = true
	r := &bootstrapBucketFake{headErr: &aws.Error{Code: "404"}}
	if ensureStateBucket(r, o, "123456789012") == nil || len(r.calls) != 1 {
		t.Fatal("missing initialized state replaced", r.calls)
	}
}
