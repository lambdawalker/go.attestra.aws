package workflow

import (
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/android"
)

func exportAndroidConfiguration(r commandRunner, o options, path string) error {
	deployed, err := publicIndexConfiguration(r, o)
	if err != nil {
		return err
	}
	raw, err := r.Exec(filepath.Join(o.Root, "infra"), true, "pulumi", "config", "--json", "--stack", o.Stack)
	if err != nil {
		return err
	}
	var config map[string]struct{ Value string }
	if json.Unmarshal(raw, &config) != nil {
		return errors.New("cannot read Android configuration")
	}
	origin, err := url.Parse(config["attestra-application:appOrigin"].Value)
	if err != nil || !healthURL(origin.String()) {
		return errors.New("valid appOrigin is required for Android link handling")
	}
	kind := config["attestra-application:captureDocumentType"].Value
	if kind == "" {
		kind = "sample_card"
	}
	return android.Write(path, o.Stack, origin.Hostname(), kind, deployed)
}

func defaultAndroidExport(o options) string {
	return filepath.Join(o.Root, "android-config", o.Stack+".properties")
}
