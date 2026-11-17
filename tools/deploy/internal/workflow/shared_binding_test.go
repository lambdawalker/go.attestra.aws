package workflow

import (
	"testing"
)

func TestSharedBindingRejectsAccountRegionAndDependencyMismatch(t *testing.T) {
	out := sharedStackOutputs{RuntimeTableName: "runtime", RuntimeTableARN: "arn:aws:dynamodb:us-east-2:123456789012:table/runtime", APIARN: "arn:aws:execute-api:us-east-2:123456789012:index", APIURL: "https://index.execute-api.us-east-2.amazonaws.com"}
	binding, e := validateSharedBinding("123456789012", "us-east-2", out)
	if e != nil {
		t.Fatal(e)
	}
	cfg := map[string]struct{ Value string }{"attestra-application:runtimeTableName": {"runtime"}, "attestra-application:runtimeTableArn": {out.RuntimeTableARN}, "attestra-application:indexApiArn": {out.APIARN}, "attestra-application:indexApiUrl": {out.APIURL}}
	if checkSharedBackendBinding(cfg, binding) != nil {
		t.Fatal("matching binding rejected")
	}
	cfg["attestra-application:runtimeTableName"] = struct{ Value string }{"different"}
	if checkSharedBackendBinding(cfg, binding) == nil {
		t.Fatal("different shared deployment adopted")
	}
	if _, e = validateSharedBinding("999999999999", "us-east-2", out); e == nil {
		t.Fatal("cross-account binding accepted")
	}
	if _, e = validateSharedBinding("123456789012", "us-west-2", out); e == nil {
		t.Fatal("cross-region binding accepted")
	}
}
