package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/lambdawalker/go.apexfission.aws.auth/assetlinks"
)

// Import only public associations. Empty input leaves any existing website alone.
func (w *bootstrapWizard) associationDefaults(current string, defaults map[string]string, choose func(string, string, []string) (string, error), confirm func(string) (bool, error)) error {
	var existing []assetlinks.Application
	if current != "" {
		if json.Unmarshal([]byte(current), &existing) != nil {
			return errors.New("invalid androidAssociations JSON")
		}
		if _, err := assetlinks.Render(existing); err != nil {
			return err
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths, discoveryErr := discoverAssociationFiles(home)
	if discoveryErr != nil {
		fmt.Printf("Could not search local Android exports: %v. Enter a public JSON path manually.\n", discoveryErr)
	}
	if discoveryErr == nil && len(paths) == 0 && !w.forceSetup {
		if current != "" {
			return nil
		}
		if _, known := w.remembered("androidAssociationsSkipped"); known {
			return nil
		}
	}
	path, err := choose(home, w.environment, paths)
	if err != nil {
		return err
	}
	if path == "" {
		if current != "" {
			return nil
		}
		return w.remember("androidAssociationsSkipped", "true")
	}
	incoming, err := readAssociationFile(path)
	if err != nil {
		return fmt.Errorf("public association file %s: %w", path, err)
	}
	merged, changed, err := mergeAssociations(existing, incoming)
	if err != nil {
		return err
	}
	showAssociationFile(path, w.environment, incoming)
	if !changed {
		fmt.Println("These public signing identities are already configured.")
		return nil
	}
	approve, err := confirm(w.environment)
	if err != nil {
		return err
	}
	if approve {
		compact, _ := json.Marshal(merged)
		defaults["attestra-application:androidAssociations"] = string(compact)
	}
	return nil
}

// Merge only additions; a local handoff cannot revoke existing identities.
func mergeAssociations(existing, incoming []assetlinks.Application) ([]assetlinks.Application, bool, error) {
	if _, err := assetlinks.Render(existing); err != nil {
		return nil, false, err
	}
	if _, err := assetlinks.Render(incoming); err != nil {
		return nil, false, err
	}
	data, _ := json.Marshal(existing)
	var merged []assetlinks.Application
	_ = json.Unmarshal(data, &merged)
	changed := false
	for _, app := range incoming {
		index := -1
		for i := range merged {
			if merged[i].PackageName == app.PackageName {
				index = i
				break
			}
		}
		if index < 0 {
			merged = append(merged, app)
			changed = true
			continue
		}
		for _, fp := range app.Fingerprints {
			found := false
			for _, old := range merged[index].Fingerprints {
				if strings.EqualFold(old, fp) {
					found = true
					break
				}
			}
			if !found {
				merged[index].Fingerprints = append(merged[index].Fingerprints, strings.ToUpper(fp))
				changed = true
			}
		}
	}
	if _, err := assetlinks.Render(merged); err != nil {
		return nil, false, err
	}
	return merged, changed, nil
}

func associationTargets(stack string, route53 bool) []string {
	prefix := "urn:pulumi:" + stack + "::attestra-application::"
	targets := []string{prefix + "pulumi:providers:aws::app-edge", prefix + "aws:acm/certificate:Certificate::app-certificate"}
	if route53 {
		targets = append(targets, prefix+"aws:route53/record:Record::app-certificate-dns")
	}
	return targets
}
func checkAssetLinks(client *http.Client, endpoint, expected string) error {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/.well-known/assetlinks.json" {
		return errors.New("invalid asset links URL")
	}
	probe := *client
	probe.Timeout = 15 * time.Second
	probe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, e := probe.Get(endpoint)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != 200 || media != "application/json" {
		return fmt.Errorf("asset links require HTTP 200 application/json without redirects (got %d %s)", response.StatusCode, media)
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, 1_048_577))
	if e != nil {
		return e
	}
	if len(data) > 1_048_576 {
		return errors.New("oversized asset links document")
	}
	var actual, want any
	if json.Unmarshal(data, &actual) != nil || json.Unmarshal([]byte(expected), &want) != nil || !reflect.DeepEqual(actual, want) {
		return errors.New("public asset links differ from approved deployed associations")
	}
	return nil
}
func waitAssetLinks(r commandRunner, o options) error {
	data, e := r.Exec(filepath.Join(o.Root, "infra"), true, "pulumi", "stack", "output", "--json", "--stack", o.Stack)
	if e != nil {
		return e
	}
	var out struct {
		URL     string `json:"assetLinksUrl"`
		Content string `json:"assetLinksContent"`
	}
	if json.Unmarshal(data, &out) != nil {
		return errors.New("invalid stack outputs")
	}
	if out.URL == "" {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	for {
		err := checkAssetLinks(&http.Client{}, out.URL, out.Content)
		if err == nil {
			fmt.Println("✓ Android associations ready: " + out.URL)
			return nil
		}
		fmt.Printf("Waiting for Android association hosting: %v; checking again in 30 seconds.\n", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("Android association readiness stopped; resources retained: %w", err)
		case <-time.After(30 * time.Second):
		}
	}
}
