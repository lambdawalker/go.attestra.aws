package workflow

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func validateSharedBinding(account, region string, out sharedStackOutputs) (SharedBinding, error) {
	name, e := validateSnapshotTable(out.RuntimeTableARN, account, region)
	if e != nil || name != out.RuntimeTableName {
		return SharedBinding{}, errors.New("shared runtime table outputs do not match account/region/name")
	}
	prefix := "arn:aws:execute-api:" + region + ":" + account + ":"
	id := strings.TrimPrefix(out.APIARN, prefix)
	if id == out.APIARN || !regexp.MustCompile(`^[a-z0-9]+$`).MatchString(id) {
		return SharedBinding{}, errors.New("shared index API ARN does not match account/region/API identity")
	}
	u, e := url.Parse(out.APIURL)
	if e != nil || u.Scheme != "https" || u.Hostname() != id+".execute-api."+region+".amazonaws.com" || u.Port() != "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return SharedBinding{}, errors.New("invalid shared index API URL")
	}
	return SharedBinding{1, account, region, name, out.RuntimeTableARN, out.APIARN, out.APIURL}, nil
}
func readSharedBinding(root string) (SharedBinding, error) {
	var b SharedBinding
	data, e := os.ReadFile(filepath.Join(root, ".deploy", "shared-binding.json"))
	if e != nil {
		return b, e
	}
	if json.Unmarshal(data, &b) != nil || b.Version != 1 {
		return b, errors.New("invalid shared binding receipt")
	}
	return b, nil
}
func checkSharedBackendBinding(config map[string]struct{ Value string }, b SharedBinding) error {
	for k, v := range map[string]string{"runtimeTableName": b.RuntimeTableName, "runtimeTableArn": b.RuntimeTableARN, "indexApiArn": b.APIARN, "indexApiUrl": b.APIURL} {
		if config["attestra-application:"+k].Value != v {
			return errors.New("backend " + k + " does not match the selected shared deployment; reconcile existing bindings explicitly before deploying")
		}
	}
	return nil
}
