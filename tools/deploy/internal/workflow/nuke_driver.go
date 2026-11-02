package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/dns"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

type composerNukeDriver struct {
	Base                                 *pulumicli.Driver
	Root, Repository, Account, CallerARN string
	Env                                  []string
	GitHub                               *github.Client
	Cloudflare                           *dns.Client
	Metadata                             repositoryMetadata
	AWSForRegion                         func(string) awsenv.API
	BeforeInventory                      func(teardown.Target) error
	BeforeMutation                       func() error
	extras                               []teardown.Artifact
	discoveryLoaded                      bool
	knownTargets                         []teardown.Target
	inventoryFailed                      map[string]bool
}

func (d *composerNukeDriver) api(region string) awsenv.API {
	if d.AWSForRegion != nil {
		return d.AWSForRegion(region)
	}
	return &awsenv.Client{Env: replaceEnvironment(d.Env, map[string]string{"AWS_REGION": region, "AWS_DEFAULT_REGION": region})}
}
func (d *composerNukeDriver) identity() error {
	var v struct{ Account string }
	ok, e := d.api("us-east-1").Call(&v, "sts", "get-caller-identity")
	if e != nil || !ok || v.Account != d.Account {
		return errors.New("AWS account verification failed")
	}
	return nil
}

// DiscoverArtifacts must run before the engine's initial Store.Save so external
// evidence survives even when no Pulumi stacks remain. It never changes cloud state.
func (d *composerNukeDriver) DiscoverArtifacts(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if d.discoveryLoaded {
		return nil
	}
	if e := d.identity(); e != nil {
		return e
	}
	found, e := collectNukeDiscovery(d.Root, d.Repository, d.Account, d.GitHub, d.api)
	if e != nil {
		return e
	}
	d.extras = found.artifacts
	if e := d.discoverRoles(); e != nil {
		return e
	}
	d.discoveryLoaded = true
	return nil
}
func (d *composerNukeDriver) AdditionalArtifacts() []teardown.Artifact {
	return append([]teardown.Artifact(nil), d.extras...)
}
func (d *composerNukeDriver) Discover(ctx context.Context, s teardown.Scope) ([]teardown.Target, error) {
	if s.Account != d.Account || s.Application != "attestra" {
		return nil, errors.New("nuke scope mismatch")
	}
	if e := d.DiscoverArtifacts(ctx); e != nil {
		return nil, e
	}
	targets, e := d.Base.Discover(ctx, s)
	for _, target := range targets {
		d.trackTarget(target)
	}
	return targets, e
}
func (d *composerNukeDriver) Inventory(ctx context.Context, t teardown.Target) (artifacts []teardown.Artifact, inventoryErr error) {
	d.trackTarget(t)
	if d.inventoryFailed == nil {
		d.inventoryFailed = map[string]bool{}
	}
	defer func() { d.inventoryFailed[t.ID] = inventoryErr != nil && !errors.Is(inventoryErr, teardown.ErrAbsent) }()
	if d.BeforeInventory != nil {
		if e := d.BeforeInventory(t); e != nil {
			return nil, errors.New("stack credentials unavailable")
		}
	}
	artifacts, e := d.Base.Inventory(ctx, t)
	if e != nil {
		return artifacts, e
	}
	// The Composer adapter owns cloud inventory and destruction; the app reads a
	// second export solely for Cloudflare record evidence outside Pulumi.
	b, e := d.Base.Runner.Run(ctx, t.Directory, []string{"PULUMI_BACKEND_URL=" + t.Backend, "AWS_REGION=" + t.Region, "AWS_DEFAULT_REGION=" + t.Region}, "pulumi", "stack", "export", "--stack", t.Stack, "--non-interactive")
	if e != nil {
		return artifacts, errors.New("DNS evidence export unavailable")
	}
	key, e := nukePulumiKey(b, t.Region)
	if e != nil {
		return artifacts, e
	}
	if key.ID != "" {
		evidence, err := nukeKeyEvidence(d.api(key.Region), d.Account, key.Region, key.ID)
		if err != nil {
			return artifacts, err
		}
		approved := false
		for _, known := range d.Base.Config.Recovery {
			if known.Kind == "recovery_key" && known.ID == evidence.ID && known.Region == evidence.Region && known.Recovery && known.Data["exclusive"] == "true" {
				approved = true
			}
		}
		if evidence.ID != "" && !approved {
			evidence.Kind = "attestra_retained_key"
			evidence.Recovery = false
			evidence.TargetID = t.ID
			evidence.Data["account"] = d.Account
			evidence.Data["repository"] = d.Repository
			artifacts = append(artifacts, evidence)
		}
	}
	wants, e := nukeDNSInventory(b)
	if e != nil {
		return artifacts, e
	}
	for _, want := range wants {
		data := map[string]string{"type": want.Type, "content": want.Content, "name": dns.Name(want.Name), "account": d.Account, "repository": d.Repository, "cleanup": "true"}
		if d.Cloudflare != nil {
			zones, e := d.Cloudflare.Zones(want.Name)
			if e != nil {
				return artifacts, errors.New("DNS zone discovery failed")
			}
			if len(zones) != 1 {
				return artifacts, errors.New("DNS zone ownership ambiguous")
			}
			data["zone"] = zones[0].ID
		}
		artifacts = append(artifacts, teardown.Artifact{ID: fmt.Sprintf("%s|%s|%x", dns.Name(want.Name), want.Type, sha256.Sum256([]byte(want.Content))), Kind: "attestra_dns", Region: t.Region, TargetID: t.ID, Data: data})
	}
	return artifacts, nil
}
func (d *composerNukeDriver) Destroy(ctx context.Context, t teardown.Target) error {
	if e := d.beforeMutation(); e != nil {
		return e
	}
	return d.Base.Destroy(ctx, t)
}
func (d *composerNukeDriver) RemoveRecovery(ctx context.Context, a teardown.Artifact) error {
	if e := d.beforeMutation(); e != nil {
		return e
	}
	return d.Base.RemoveRecovery(ctx, a)
}
func (d *composerNukeDriver) PrepareRecovery(ctx context.Context, artifacts []teardown.Artifact) error {
	if e := d.beforeMutation(); e != nil {
		return e
	}
	if d.hasInventoryFailure() {
		return errors.New("application inventory remains unverifiable")
	}
	// Primary marker cleanup runs through ArtifactCleaner. Never bypass Composer
	// object-scope checks, and never remove another operation's bootstrap lock.
	for _, bucket := range artifacts {
		if bucket.Kind != "recovery_bucket" || !strings.HasPrefix(bucket.ID, "attestra-control-plane-state-"+d.Account+"-") {
			continue
		}
		for _, key := range []string{"bootstrap-ready.json", "teardown-pending.json", "bootstrap.lock"} {
			marker := teardown.Artifact{ID: bucket.ID + "/" + key, Kind: "attestra_marker", Region: bucket.Region, Data: map[string]string{"bucket": bucket.ID, "key": key, "account": d.Account, "project": "attestra-control-plane", "stack": "shared"}}
			state, e := d.Inspect(ctx, marker)
			if e != nil || state != teardown.StateAbsent {
				return errors.New("shared markers or bootstrap lock remain")
			}
		}
	}
	return d.Base.PrepareRecovery(ctx, artifacts)
}
func (d *composerNukeDriver) appArtifact(a teardown.Artifact) error {
	if a.Data["account"] != d.Account {
		return errors.New("artifact account mismatch")
	}
	if a.Kind != "attestra_marker" && a.Data["repository"] != d.Repository {
		return errors.New("artifact repository mismatch")
	}
	return nil
}
func (d *composerNukeDriver) Inspect(ctx context.Context, a teardown.Artifact) (teardown.State, error) {
	if !strings.HasPrefix(a.Kind, "attestra_") {
		if d.Base == nil {
			return "", errors.New("saved artifact Pulumi route unavailable")
		}
		return d.Base.Inspect(ctx, a)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if e := d.identity(); e != nil {
		return "", e
	}
	if e := d.appArtifact(a); e != nil {
		return "", e
	}
	switch a.Kind {
	case "attestra_retained_key":
		if d.Base == nil {
			return "", errors.New("saved KMS verification route unavailable")
		}
		a.Kind = "recovery_key"
		return d.Base.Inspect(ctx, a)
	case "attestra_dns":
		records, _, e := d.dnsMatches(a)
		if e != nil {
			return "", e
		}
		if len(records) == 0 {
			return teardown.StateAbsent, nil
		}
		return teardown.StatePresent, nil
	case "attestra_github_environment":
		names, e := nukeEnvironmentNames(d.GitHub, d.Repository)
		if e != nil {
			return "", e
		}
		for _, name := range names {
			if name == a.ID {
				return teardown.StatePresent, nil
			}
		}
		return teardown.StateAbsent, nil
	case "attestra_deployment_role":
		if e := d.roleIdentity(a); e != nil {
			return "", e
		}
		var body struct{ Role struct{ Arn string } }
		ok, e := d.api(a.Region).Call(&body, "iam", "get-role", "--role-name", strings.Split(a.ID, ":role/")[1])
		if nukeAWSMissing(e, "NoSuchEntity") {
			return teardown.StateAbsent, nil
		}
		if e != nil {
			return "", errors.New("deployment role verification failed")
		}
		if !ok {
			return teardown.StateAbsent, nil
		}
		if body.Role.Arn != a.ID {
			return "", errors.New("deployment role identity incomplete")
		}
		return teardown.StatePresent, nil
	case "attestra_marker":
		if e := d.markerIdentity(a); e != nil {
			return "", e
		}

		api := d.api(a.Region)
		ok, e := api.Call(nil, "s3api", "head-object", "--bucket", a.Data["bucket"], "--key", a.Data["key"], "--expected-bucket-owner", d.Account)
		if nukeAWSMissing(e, "NoSuchBucket") {
			return teardown.StateAbsent, nil
		}
		if e != nil && !nukeAWSMissing(e, "NoSuchKey", "404", "NotFound") {
			return "", errors.New("shared marker verification failed")
		}
		if e == nil && ok {
			return teardown.StatePresent, nil
		}
		if e == nil && !ok {
			return "", errors.New("shared marker response incomplete")
		}
		versions, e := d.markerVersions(a, "")
		if e != nil {
			return "", e
		}
		if len(versions) > 0 {
			return teardown.StatePresent, nil
		}
		return teardown.StateAbsent, nil
	default:
		return "", errors.New("unknown application artifact")
	}
}
func (d *composerNukeDriver) dnsMatches(a teardown.Artifact) ([]dns.Record, string, error) {
	if d.Cloudflare == nil || a.ID == "" || a.Data["type"] == "" || a.Data["content"] == "" {
		return nil, "", errors.New("DNS verification evidence unavailable")
	}
	name := a.Data["name"]
	if name == "" {
		name = a.ID
	}
	zone := a.Data["zone"]
	if zone == "" {
		// Resolve only the preserved record name. This is resource read-back, not
		// discovery of new application targets, and remains safe for full/fast checks.
		zones, e := d.Cloudflare.Zones(name)
		if e != nil || len(zones) != 1 {
			return nil, "", errors.New("DNS zone ownership unavailable or ambiguous")
		}
		zone = zones[0].ID
	}
	records, e := d.Cloudflare.Records(zone, name)
	if e != nil {
		return nil, "", errors.New("DNS read-back failed")
	}
	records, e = dns.DeletionCandidates(dns.Record{Name: name, Type: a.Data["type"], Content: a.Data["content"]}, records)
	if e != nil {
		return nil, "", errors.New("DNS ownership conflict")
	}
	return records, zone, nil
}
func (d *composerNukeDriver) roleIdentity(a teardown.Artifact) error {
	parts := strings.Split(a.ID, ":")
	if len(parts) != 6 || parts[2] != "iam" || parts[4] != d.Account || !strings.HasPrefix(parts[5], "role/") || strings.Contains(strings.TrimPrefix(parts[5], "role/"), "/") {
		return errors.New("invalid deployment role identity")
	}
	name := strings.TrimPrefix(parts[5], "role/")
	caller := strings.Split(d.CallerARN, "/")
	if d.CallerARN == a.ID || (strings.Contains(d.CallerARN, ":assumed-role/") && len(caller) >= 2 && caller[len(caller)-2] == name) {
		return errors.New("refusing caller deployment role")
	}
	if !nukeName(a.Data["environment"]) {
		return errors.New("missing deployment environment")
	}
	return nil
}
func (d *composerNukeDriver) markerIdentity(a teardown.Artifact) error {
	if a.Data["project"] != "attestra-control-plane" || a.Data["stack"] != "shared" || !strings.HasPrefix(a.Data["bucket"], "attestra-control-plane-state-"+d.Account+"-") || a.ID != a.Data["bucket"]+"/"+a.Data["key"] {
		return errors.New("shared marker scope mismatch")
	}
	switch a.Data["key"] {
	case "bootstrap-ready.json", "teardown-pending.json", "bootstrap.lock":
		return nil
	}
	return errors.New("unknown shared marker")
}
func (d *composerNukeDriver) Cleanup(ctx context.Context, a teardown.Artifact) error {
	if e := d.beforeMutation(); e != nil {
		return e
	}
	if !strings.HasPrefix(a.Kind, "attestra_") {
		return d.Base.Cleanup(ctx, a)
	}
	if e := d.identity(); e != nil {
		return e
	}
	if e := d.appArtifact(a); e != nil {
		return e
	}
	if d.hasInventoryFailure() {
		return errors.New("application inventory remains unverifiable")
	}
	for _, target := range d.knownTargets {
		state, e := d.Base.Inspect(ctx, teardown.Artifact{ID: target.ID, Kind: "stack", Region: target.Region, TargetID: target.ID})
		if e != nil || state != teardown.StateAbsent {
			return errors.New("application stack still present or unverifiable")
		}
	}
	switch a.Kind {
	case "attestra_dns":
		records, zone, e := d.dnsMatches(a)
		if e != nil {
			return e
		}
		return d.Cloudflare.DeleteDNS(zone, records)
	case "attestra_github_environment":
		if a.ID != a.Data["environment"] || !nukeName(a.ID) {
			return errors.New("environment identity mismatch")
		}
		names, e := nukeEnvironmentNames(d.GitHub, d.Repository)
		if e != nil {
			return e
		}
		present := false
		for _, name := range names {
			present = present || name == a.ID
		}
		if !present {
			return nil
		}
		// A new environment in another account must never be removed by an old receipt.
		snapshot, e := d.GitHub.InspectEnvironment(d.Repository, a.ID)
		if e != nil || !snapshot.Exists || snapshot.Variables["AWS_ACCOUNT_ID"] != d.Account {
			return errors.New("environment account changed or inaccessible")
		}
		_, e = d.GitHub.Request("DELETE", github.EnvironmentPath(d.Repository, a.ID), nil, nil)
		if e != nil {
			return errors.New("environment deletion failed")
		}
		return nil
	case "attestra_deployment_role":
		if e := d.roleIdentity(a); e != nil {
			return e
		}
		if d.Metadata.ID <= 0 || d.Metadata.Owner.ID <= 0 || d.Metadata.Owner.Login+"/"+d.Metadata.Name != d.Repository {
			return errors.New("immutable repository identity unavailable")
		}
		// Legacy mutable repository subjects are deliberately insufficient here.
		if e := d.immutableRole(a); e != nil {
			return e
		}
		if e := deleteTeardownRole(nukeStrictRoleAPI{d.api(a.Region)}, a.ID, a.Data["environment"], d.Metadata); e != nil {
			return errors.New("deployment role deletion refused or failed")
		}
		return nil
	case "attestra_retained_key":
		return errors.New("KMS exclusivity unproven; key and state retained")
	case "attestra_marker":
		return d.cleanupMarker(ctx, a)
	default:
		return errors.New("unknown application cleanup artifact")
	}
}
func (d *composerNukeDriver) immutableRole(a teardown.Artifact) error {
	var body struct{ Role awsRoleSnapshot }
	ok, e := d.api(a.Region).Call(&body, "iam", "get-role", "--role-name", strings.Split(a.ID, ":role/")[1])
	if e != nil || !ok {
		return errors.New("deployment role ownership unavailable")
	}
	expected := "repo:" + d.Metadata.Owner.Login + "@" + itoa64Nuke(d.Metadata.Owner.ID) + "/" + d.Metadata.Name + "@" + itoa64Nuke(d.Metadata.ID) + ":environment:" + a.Data["environment"]
	for _, tag := range body.Role.Tags {
		if tag.Key == "attestra-subject" && tag.Value == expected {
			return nil
		}
	}
	return errors.New("deployment role lacks immutable repository binding")
}
func itoa64Nuke(v int64) string { b, _ := json.Marshal(v); return string(b) }

type nukeMarkerVersion struct {
	Key, VersionId string
	Delete         bool
}

func (d *composerNukeDriver) markerVersions(a teardown.Artifact, exclude string) ([]nukeMarkerVersion, error) {
	api := d.api(a.Region)
	var out []nukeMarkerVersion
	keyMarker, versionMarker := "", ""
	seen := map[string]bool{}
	for {
		var page struct {
			Versions, DeleteMarkers            []struct{ Key, VersionId string }
			IsTruncated                        *bool
			NextKeyMarker, NextVersionIdMarker string
		}
		args := []string{"s3api", "list-object-versions", "--bucket", a.Data["bucket"], "--prefix", a.Data["key"], "--expected-bucket-owner", d.Account, "--no-paginate"}
		if keyMarker != "" {
			args = append(args, "--key-marker", keyMarker)
			if versionMarker != "" {
				args = append(args, "--version-id-marker", versionMarker)
			}
		}
		ok, e := api.Call(&page, args...)
		if nukeAWSMissing(e, "NoSuchBucket") {
			return nil, nil
		}
		if e != nil || !ok || page.IsTruncated == nil {
			return nil, errors.New("shared marker versions unavailable")
		}
		for _, group := range []struct {
			Rows    []struct{ Key, VersionId string }
			Deleted bool
		}{{page.Versions, false}, {page.DeleteMarkers, true}} {
			for _, v := range group.Rows {
				if v.Key == "" || v.VersionId == "" {
					return nil, errors.New("incomplete marker version identity")
				}
				if v.Key == a.Data["key"] && v.VersionId != exclude {
					out = append(out, nukeMarkerVersion{v.Key, v.VersionId, group.Deleted})
				}
			}
		}
		if !*page.IsTruncated {
			return out, nil
		}
		next := page.NextKeyMarker + "\x00" + page.NextVersionIdMarker
		if page.NextKeyMarker == "" || seen[next] {
			return nil, errors.New("shared marker pagination incomplete")
		}
		seen[next] = true
		keyMarker = page.NextKeyMarker
		versionMarker = page.NextVersionIdMarker
	}
}
func (d *composerNukeDriver) cleanupMarker(ctx context.Context, a teardown.Artifact) (result error) {
	if e := d.markerIdentity(a); e != nil {
		return e
	}
	_, owned, e := nukeOwnedBucket(d.api, d.Account, a.Data["bucket"])
	if e != nil || !owned {
		return errors.New("shared bucket ownership unavailable")
	}
	tmp, e := os.MkdirTemp("", "attestra-nuke-marker-")
	if e != nil {
		return errors.New("marker temporary storage unavailable")
	}
	defer os.RemoveAll(tmp)
	body := filepath.Join(tmp, "lease.json")
	if os.WriteFile(body, []byte(`{"operation":"nuke-marker-cleanup"}`), 0600) != nil {
		return errors.New("marker temporary storage unavailable")
	}
	api := d.api(a.Region)
	var lease struct{ ETag, VersionId string }
	ok, e := api.Call(&lease, "s3api", "put-object", "--bucket", a.Data["bucket"], "--key", "bootstrap.lock", "--body", body, "--if-none-match", "*", "--expected-bucket-owner", d.Account)
	if e != nil || !ok || lease.ETag == "" {
		return errors.New("active or foreign bootstrap lock retained")
	}
	// Remove only the exact lease version we created, without creating a new
	// delete marker. Preserve any preexisting lock version until inspected below.
	defer func() {
		args := []string{"s3api", "delete-object", "--bucket", a.Data["bucket"], "--key", "bootstrap.lock", "--if-match", lease.ETag, "--expected-bucket-owner", d.Account}
		if lease.VersionId != "" {
			args = append(args, "--version-id", lease.VersionId)
		}
		if _, e := api.Call(nil, args...); e != nil {
			result = errors.Join(result, errors.New("shared bootstrap lock release failed"))
		}
	}()
	if lease.VersionId == "" {
		return errors.New("shared lock version identity unavailable; history retained")
	}
	exclude := ""
	if a.Data["key"] == "bootstrap.lock" {
		exclude = lease.VersionId
	}
	versions, e := d.markerVersions(a, exclude)
	if e != nil {
		return e
	}
	path := filepath.Join(tmp, "marker.json")
	// Preflight every payload before deleting any historical marker version.
	for _, v := range versions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if v.Delete {
			continue
		}
		ok, e = api.Call(nil, "s3api", "get-object", "--bucket", a.Data["bucket"], "--key", v.Key, "--version-id", v.VersionId, "--expected-bucket-owner", d.Account, path)
		if e != nil || !ok {
			return errors.New("shared marker version inaccessible")
		}
		b, readErr := os.ReadFile(path)
		var marker map[string]json.RawMessage
		if readErr != nil || json.Unmarshal(b, &marker) != nil || !nukeMarkerSchema(a, marker, d.Account) {
			return errors.New("shared marker version scope unverifiable")
		}
	}
	for len(versions) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		count := min(1000, len(versions))
		objects := make([]map[string]string, 0, count)
		for _, v := range versions[:count] {
			objects = append(objects, map[string]string{"Key": v.Key, "VersionId": v.VersionId})
		}
		payload, _ := json.Marshal(map[string]any{"Objects": objects, "Quiet": true})
		var reply struct{ Errors []json.RawMessage }
		ok, e = api.Call(&reply, "s3api", "delete-objects", "--bucket", a.Data["bucket"], "--expected-bucket-owner", d.Account, "--delete", string(payload))
		if e != nil || !ok || len(reply.Errors) > 0 {
			return errors.New("shared marker version deletion failed")
		}
		versions = versions[count:]
	}
	return nil
}
func nukeMarkerSchema(a teardown.Artifact, m map[string]json.RawMessage, account string) bool {
	key := a.Data["key"]
	if key == "bootstrap-ready.json" {
		if len(m) != 3 {
			return false
		}
		for _, k := range []string{"sourceHash", "domain", "zone"} {
			var v string
			if json.Unmarshal(m[k], &v) != nil || v == "" {
				return false
			}
		}
		return true
	}
	if key == "teardown-pending.json" {
		var p teardownProgress
		b, _ := json.Marshal(m)
		if json.Unmarshal(b, &p) != nil {
			return false
		}
		return p.Environment == "shared" && p.Values["account"] == account && p.Values["region"] == a.Region && p.Values["backend"] == "s3://"+a.Data["bucket"]
	}
	if key == "bootstrap.lock" {
		var operation string
		if json.Unmarshal(m["operation"], &operation) == nil && len(m) == 1 {
			return operation == "deploy-shared" || operation == "teardown-index" || operation == "nuke-marker-cleanup"
		}
		var environment, startedAt string
		if len(m) != 2 || json.Unmarshal(m["environment"], &environment) != nil || !nukeName(environment) || json.Unmarshal(m["startedAt"], &startedAt) != nil {
			return false
		}
		_, e := time.Parse(time.RFC3339Nano, startedAt)
		return e == nil
	}
	return false
}

// Capture app-specific DNS desires from managed resources only. Route53 names
// are excluded, including aliases, because their stack owns their lifecycle.
func nukeDNSInventory(b []byte) ([]dns.Record, error) {
	var state struct {
		Deployment *struct {
			Resources []struct {
				Type, URN string
				External  bool
				Outputs   map[string]json.RawMessage
			}
		}
	}
	if json.Unmarshal(b, &state) != nil || state.Deployment == nil || state.Deployment.Resources == nil {
		return nil, errors.New("invalid DNS inventory")
	}
	managed := map[string]bool{}
	managedNames := map[string]bool{}
	var wants []dns.Record
	domain, token := "", ""
	var dkim []string
	for _, r := range state.Deployment.Resources {
		if r.External {
			continue
		}
		value := func(k string) string { var v string; _ = json.Unmarshal(r.Outputs[k], &v); return v }
		switch r.Type {
		case "aws:route53/record:Record":
			managed[dns.Name(value("name"))+"/"+value("type")] = true
			managedNames[dns.Name(value("name"))] = true
		case "aws:ses/domainIdentity:DomainIdentity":
			domain = value("domain")
			token = value("verificationToken")
		case "aws:ses/domainDkim:DomainDkim":
			if raw := r.Outputs["dkimTokens"]; len(raw) > 0 && json.Unmarshal(raw, &dkim) != nil {
				return nil, errors.New("invalid SES DNS evidence")
			}
		case "aws:acm/certificate:Certificate":
			var options []struct{ ResourceRecordName, ResourceRecordType, ResourceRecordValue string }
			if raw := r.Outputs["domainValidationOptions"]; len(raw) > 0 {
				if json.Unmarshal(raw, &options) != nil {
					return nil, errors.New("invalid certificate DNS evidence")
				}
				for _, v := range options {
					wants = append(wants, dns.Record{Name: v.ResourceRecordName, Type: v.ResourceRecordType, Content: v.ResourceRecordValue})
				}
			}
		case "aws:apigatewayv2/domainName:DomainName":
			var c struct{ TargetDomainName string }
			if raw := r.Outputs["domainNameConfiguration"]; len(raw) > 0 && json.Unmarshal(raw, &c) != nil {
				return nil, errors.New("invalid API DNS evidence")
			}
			if value("domainName") != "" && c.TargetDomainName != "" {
				wants = append(wants, dns.Record{Name: value("domainName"), Type: "CNAME", Content: c.TargetDomainName})
			}
		case "aws:cloudfront/distribution:Distribution":
			var aliases []string
			if raw := r.Outputs["aliases"]; len(raw) > 0 && json.Unmarshal(raw, &aliases) != nil {
				return nil, errors.New("invalid CDN DNS evidence")
			}
			for _, name := range aliases {
				wants = append(wants, dns.Record{Name: name, Type: "CNAME", Content: value("domainName")})
			}
		}
	}
	if domain != "" {
		if token != "" {
			wants = append(wants, dns.Record{Name: "_amazonses." + domain, Type: "TXT", Content: token})
		}
		for _, v := range dkim {
			wants = append(wants, dns.Record{Name: v + "._domainkey." + domain, Type: "CNAME", Content: v + ".dkim.amazonses.com"})
		}
	}
	seen := map[string]bool{}
	out := []dns.Record{}
	for _, w := range wants {
		if w.Name == "" || w.Type == "" || w.Content == "" {
			return nil, errors.New("incomplete DNS inventory")
		}
		key := dns.Name(w.Name) + "/" + w.Type
		if managed[key] || (w.Type == "CNAME" && managedNames[dns.Name(w.Name)]) || seen[key+"/"+w.Content] {
			continue
		}
		seen[key+"/"+w.Content] = true
		out = append(out, w)
	}
	return out, nil
}

func nukePulumiKey(b []byte, region string) (teardown.Artifact, error) {
	var x struct {
		Deployment struct {
			Secrets struct {
				Type  string
				State json.RawMessage
			} `json:"secrets_providers"`
		}
	}
	if json.Unmarshal(b, &x) != nil {
		return teardown.Artifact{}, errors.New("invalid secrets provider metadata")
	}
	if x.Deployment.Secrets.Type == "" || x.Deployment.Secrets.Type == "passphrase" {
		return teardown.Artifact{}, nil
	}
	raw := x.Deployment.Secrets.State
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = []byte(encoded)
	}
	var config struct{ URL string }
	if json.Unmarshal(raw, &config) != nil || !strings.HasPrefix(config.URL, "awskms://") {
		return teardown.Artifact{}, errors.New("unsupported secrets provider; preserve state for key review")
	}
	parts := strings.SplitN(strings.TrimPrefix(config.URL, "awskms://"), "?", 2)
	id, e := url.PathUnescape(parts[0])
	if e != nil || id == "" {
		return teardown.Artifact{}, errors.New("invalid KMS provider key")
	}
	if len(parts) == 2 {
		q, e := url.ParseQuery(parts[1])
		if e != nil {
			return teardown.Artifact{}, errors.New("invalid KMS provider region")
		}
		if q.Get("region") != "" {
			region = q.Get("region")
		}
	}
	return teardown.Artifact{ID: id, Region: region}, nil
}

// Recover setup role evidence even after both its GitHub environment and local
// files disappeared. Names only bound the scan; tags bind the actual repository.
func (d *composerNukeDriver) discoverRoles() error {
	marker := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Roles       *[]struct{ RoleName, Arn string }
			IsTruncated bool
			Marker      string
		}
		args := []string{"iam", "list-roles", "--no-paginate"}
		if marker != "" {
			args = append(args, "--marker", marker)
		}
		ok, e := d.api("us-east-1").Call(&page, args...)
		if e != nil || !ok || page.Roles == nil {
			return errors.New("deployment role discovery unavailable")
		}
		for _, role := range *page.Roles {
			if !strings.HasPrefix(role.RoleName, "attestra-github-deploy") {
				continue
			}
			var body struct{ Role awsRoleSnapshot }
			ok, e = d.api("us-east-1").Call(&body, "iam", "get-role", "--role-name", role.RoleName)
			if e != nil || !ok || body.Role.Arn != role.Arn {
				return errors.New("deployment role ownership discovery unavailable")
			}
			environment, subject := "", ""
			for _, tag := range body.Role.Tags {
				if tag.Key == "attestra-setup" && strings.HasPrefix(tag.Value, "github-") {
					environment = strings.TrimPrefix(tag.Value, "github-")
				}
				if tag.Key == "attestra-subject" {
					subject = tag.Value
				}
			}
			if !nukeName(environment) {
				continue
			}
			legacy := "repo:" + d.Repository + ":environment:" + environment
			immutable := "repo:" + d.Metadata.Owner.Login + "@" + itoa64Nuke(d.Metadata.Owner.ID) + "/" + d.Metadata.Name + "@" + itoa64Nuke(d.Metadata.ID) + ":environment:" + environment
			if subject != legacy && (d.Metadata.ID <= 0 || d.Metadata.Owner.ID <= 0 || subject != immutable) {
				continue
			}
			parts := strings.Split(role.Arn, ":")
			if len(parts) != 6 || parts[4] != d.Account {
				return errors.New("deployment role account mismatch")
			}
			d.extras = append(d.extras, teardown.Artifact{ID: role.Arn, Kind: "attestra_deployment_role", Region: "us-east-1", Data: map[string]string{"account": d.Account, "repository": d.Repository, "environment": environment, "cleanup": "true"}})
		}
		if !page.IsTruncated {
			return nil
		}
		if page.Marker == "" || seen[page.Marker] {
			return errors.New("deployment role pagination incomplete")
		}
		seen[page.Marker] = true
		marker = page.Marker
	}
}

// Existing role cleanup enforces allowed policies and exact trust. Tighten its
// list responses here so omitted arrays cannot masquerade as empty attachments.
type nukeStrictRoleAPI struct{ base awsenv.API }

func (a nukeStrictRoleAPI) Call(result any, args ...string) (bool, error) {
	field := ""
	if len(args) > 1 {
		switch args[1] {
		case "list-instance-profiles-for-role":
			field = "InstanceProfiles"
		case "list-role-policies":
			field = "PolicyNames"
		case "list-attached-role-policies":
			field = "AttachedPolicies"
		}
	}
	if field == "" {
		return a.base.Call(result, args...)
	}
	var raw json.RawMessage
	found, e := a.base.Call(&raw, args...)
	if e != nil {
		return found, e
	}
	if !found {
		return false, errors.New("IAM ownership listing unavailable")
	}
	var object map[string]json.RawMessage
	var rows []json.RawMessage
	var truncated bool
	if json.Unmarshal(raw, &object) != nil || object["IsTruncated"] == nil || json.Unmarshal(object["IsTruncated"], &truncated) != nil || truncated || json.Unmarshal(object[field], &rows) != nil || rows == nil {
		return false, errors.New("IAM ownership listing incomplete")
	}
	if result != nil && json.Unmarshal(raw, result) != nil {
		return false, errors.New("IAM ownership listing invalid")
	}
	return true, nil
}

// Every target presented by the engine is tracked, including targets recovered
// solely from its saved report and targets whose credential/inventory probe fails.
func (d *composerNukeDriver) trackTarget(t teardown.Target) {
	for _, known := range d.knownTargets {
		if known.ID == t.ID {
			return
		}
	}
	d.knownTargets = append(d.knownTargets, t)
}
func (d *composerNukeDriver) hasInventoryFailure() bool {
	for _, failed := range d.inventoryFailed {
		if failed {
			return true
		}
	}
	return false
}

// This guard rechecks workflow activity immediately before a mutation. Operators
// must still suspend deployments across checkouts: this is not a distributed
// lease spanning every external system and every resource mutation.
func (d *composerNukeDriver) beforeMutation() error {
	if d.BeforeMutation != nil {
		if e := d.BeforeMutation(); e != nil {
			return errors.New("deployment activity cannot be excluded; mutation refused")
		}
	}
	return nil
}
