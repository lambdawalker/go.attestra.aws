package bootstrap

import (
	"encoding/base64"
	"fmt"
	"github.com/lambdawalker/go.apexfission.aws.auth/core"
	"net/url"
)

type Settings struct {
	Mode, Environment, Issuer, ClientID, Table, Pool, Origin, Sender, ConfigTable, LoginDomain, AdminOrigin string
	ProofKey                                                                                                []byte
}

func httpsOrigin(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func ReadSettings(get func(string) string) (Settings, error) {
	s := Settings{Mode: get("MODE"), Environment: get("ENVIRONMENT"), Issuer: get("ISSUER"), ClientID: get("CLIENT_ID"), Table: get("TABLE_NAME"), Pool: get("POOL_ID"), Origin: get("APP_ORIGIN"), Sender: get("SENDER_ADDRESS"), ConfigTable: get("CONFIG_TABLE"), LoginDomain: get("LOGIN_DOMAIN"), AdminOrigin: get("ADMIN_ORIGIN")}
	if !core.IDPattern.MatchString(s.Environment) || s.ClientID == "" || s.Issuer == "" {
		return s, fmt.Errorf("ENVIRONMENT, ISSUER and CLIENT_ID are required")
	}
	switch s.Mode {
	case "auth":
		key, e := base64.StdEncoding.DecodeString(get("PROOF_KEY"))
		if e != nil || len(key) < 32 {
			return s, fmt.Errorf("PROOF_KEY must be base64 of at least 32 bytes")
		}
		s.ProofKey = key
		if s.Table == "" || s.Pool == "" || s.Sender == "" || !httpsOrigin(s.Origin) {
			return s, fmt.Errorf("auth requires TABLE_NAME, POOL_ID, SENDER_ADDRESS and HTTPS APP_ORIGIN")
		}
	case "admin":
		if s.ConfigTable == "" || !httpsOrigin(s.LoginDomain) || !httpsOrigin(s.AdminOrigin) {
			return s, fmt.Errorf("admin requires CONFIG_TABLE, HTTPS LOGIN_DOMAIN and ADMIN_ORIGIN")
		}
	default:
		return s, fmt.Errorf("MODE must be auth or admin")
	}
	return s, nil
}
