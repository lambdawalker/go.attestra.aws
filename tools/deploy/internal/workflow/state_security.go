package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

func verifyStateSecurity(r commandRunner, o options, account string) error {
	u, _ := url.Parse(o.Backend)
	read := func(operation string, result any) error {
		b, e := r.Exec(o.Root, true, "aws", "s3api", operation, "--bucket", u.Host, "--expected-bucket-owner", account, "--output", "json")
		if e != nil {
			return fmt.Errorf("state bucket %s check failed: %w", operation, e)
		}
		if json.Unmarshal(b, result) != nil {
			return fmt.Errorf("invalid state bucket %s response", operation)
		}
		return nil
	}
	var loc struct{ LocationConstraint *string }
	if e := read("get-bucket-location", &loc); e != nil {
		return e
	}
	region := "us-east-1"
	if loc.LocationConstraint != nil {
		region = *loc.LocationConstraint
		if region == "EU" {
			region = "eu-west-1"
		}
	}
	if region != o.Region {
		return errors.New("state bucket region differs from target; no settings changed")
	}
	var encryption struct {
		ServerSideEncryptionConfiguration struct {
			Rules []struct{ ApplyServerSideEncryptionByDefault struct{ SSEAlgorithm string } }
		}
	}
	if e := read("get-bucket-encryption", &encryption); e != nil {
		return e
	}
	secure := false
	for _, rule := range encryption.ServerSideEncryptionConfiguration.Rules {
		switch rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm {
		case "AES256", "aws:kms", "aws:kms:dsse":
			secure = true
		}
	}
	if !secure {
		return errors.New("state bucket encryption is missing or unsupported; repair existing storage deliberately")
	}
	var policy struct{ Policy string }
	if e := read("get-bucket-policy", &policy); e != nil {
		return e
	}
	if !deniesInsecureState(policy.Policy, u.Host) {
		return errors.New("state bucket must deny non-TLS access to bucket and objects; preserve existing policy statements when adding this protection")
	}
	return nil
}
func stringList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, i := range x {
			if s, ok := i.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
func deniesInsecureState(raw, bucket string) bool {
	var p struct {
		Statement []struct {
			Effect           string
			Principal        any
			Action, Resource any
			Condition        map[string]map[string]any
		}
	}
	if json.Unmarshal([]byte(raw), &p) != nil {
		return false
	}
	resources := map[string]bool{}
	for _, s := range p.Statement {
		principal := s.Principal == "*"
		if m, ok := s.Principal.(map[string]any); ok {
			principal = m["AWS"] == "*"
		}
		if !principal || s.Effect != "Deny" {
			continue
		}
		transport := s.Condition["Bool"]["aws:SecureTransport"]
		if transport != "false" && transport != false {
			continue
		}
		// Extra conditions narrow the denial and cannot establish universal TLS enforcement.
		if len(s.Condition) != 1 || len(s.Condition["Bool"]) != 1 {
			continue
		}
		all := false
		for _, a := range stringList(s.Action) {
			if a == "s3:*" || a == "*" {
				all = true
			}
		}
		if !all {
			continue
		}
		for _, r := range stringList(s.Resource) {
			resources[r] = true
		}
	}
	return resources["*"] || (resources["arn:aws:s3:::"+bucket] && resources["arn:aws:s3:::"+bucket+"/*"])
}
func tlsStatePolicy(bucket string) string {
	return policyJSON(map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Sid": "DenyInsecureTransport", "Effect": "Deny", "Principal": "*", "Action": "s3:*", "Resource": []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"}, "Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "false"}}}}})
}
