package bootstrap

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRuntimeSettingsRejectMissingAndConfusedModes(t *testing.T) {
	valid := map[string]string{"MODE": "auth", "ENVIRONMENT": "dev", "ISSUER": "https://cognito-idp.us-east-1.amazonaws.com/pool", "CLIENT_ID": "client", "TABLE_NAME": "proofs", "POOL_ID": "pool", "APP_ORIGIN": "https://app.example", "SENDER_ADDRESS": "hello@example.com", "PROOF_KEY": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))}
	if _, e := ReadSettings(func(k string) string { return valid[k] }); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"MODE", "CLIENT_ID", "PROOF_KEY", "ENVIRONMENT"} {
		copy := map[string]string{}
		for k, v := range valid {
			copy[k] = v
		}
		delete(copy, key)
		if _, e := ReadSettings(func(k string) string { return copy[k] }); e == nil {
			t.Fatal("accepted missing", key)
		}
	}
	valid["MODE"] = "admin"
	if _, e := ReadSettings(func(k string) string { return valid[k] }); e == nil {
		t.Fatal("admin accepted without dedicated config")
	}
}
