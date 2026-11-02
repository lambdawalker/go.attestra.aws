package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
	"github.com/lambdawalker/go.infra.composer/ownership"
)

// State is shared by application environments. Never replace tags belonging to
// another application, and preserve unrelated operator tags on the bucket.
func labelStateBucket(a awsenv.API, o options, account string, m ownership.Metadata) error {
	u, err := url.Parse(o.Backend)
	if err != nil || u.Scheme != "s3" || u.Host == "" {
		return fmt.Errorf("state backend must be an S3 URL")
	}
	var result struct{ TagSet []struct{ Key, Value string } }
	found, err := a.Call(&result, "s3api", "get-bucket-tagging", "--bucket", u.Host, "--expected-bucket-owner", account)
	if err != nil {
		var apiErr *awsenv.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "NoSuchTagSet" {
			return err
		}
	} else if !found || result.TagSet == nil {
		return fmt.Errorf("state bucket disappeared while reading tags")
	}
	existing := map[string]string{}
	for _, t := range result.TagSet {
		existing[t.Key] = t.Value
	}
	merged, err := m.MergeTags(existing, "pulumi-state")
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tags := []map[string]string{}
	for _, k := range keys {
		tags = append(tags, map[string]string{"Key": k, "Value": merged[k]})
	}
	body, _ := json.Marshal(map[string]any{"TagSet": tags})
	found, err = a.Call(nil, "s3api", "put-bucket-tagging", "--bucket", u.Host, "--expected-bucket-owner", account, "--tagging", string(body))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("state bucket disappeared while writing tags")
	}
	return nil
}
