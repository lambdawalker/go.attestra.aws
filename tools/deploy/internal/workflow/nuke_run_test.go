package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

func TestApprovedNukeStorePreservesUnknownKeysAndGatedRealKey(t *testing.T) {
	key := teardown.Artifact{ID: "arn:aws:kms:us-east-2:123456789012:key/id", Kind: "recovery_key", Region: "us-east-2", Recovery: true, Data: map[string]string{"exclusive": "true"}}
	r := &teardown.Report{Version: 1, Scope: teardown.Scope{Application: "attestra", Account: "123456789012"}, Artifacts: []teardown.Artifact{
		{ID: key.ID, Kind: "attestra_retained_key", Region: key.Region},
		{ID: "unknown-key", Kind: "attestra_retained_key", Region: key.Region}, key,
	}}
	s := approvedNukeStore{FileStore: teardown.FileStore{Path: filepath.Join(t.TempDir(), "nuke.json")}, Recovery: func() []teardown.Artifact { return []teardown.Artifact{key} }}
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	if len(r.Artifacts) != 2 || r.Artifacts[0].ID != "unknown-key" || !r.Artifacts[1].Recovery {
		t.Fatal(r.Artifacts)
	}
	saved, err := s.Load()
	if err != nil || len(saved.Artifacts) != 2 {
		t.Fatal(saved, err)
	}
}

func TestComposerNukeFlagsAndConflicts(t *testing.T) {
	for _, supplied := range [][]string{{"teardown", "nuke"}, {"teardown", "nuke", "check"}, {"teardown", "nuke", "fast-check"}, {"teardown", "nuke", "check", "fast-check", "repo-root"}} {
		if err := validateComposerNukeFlags(true, true, supplied); err != nil {
			t.Fatal(err)
		}
	}
	for _, supplied := range [][]string{{"check"}, {"fast-check"}} {
		if validateComposerNukeFlags(false, false, supplied) == nil {
			t.Fatal("standalone check accepted")
		}
	}
	if validateComposerNukeFlags(true, false, []string{"nuke"}) == nil {
		t.Fatal("standalone nuke accepted")
	}
	for _, conflict := range []string{"stack", "scope", "ci", "bootstrap", "teardown-index", "build"} {
		if validateComposerNukeFlags(true, true, []string{"teardown", "nuke", conflict}) == nil {
			t.Fatal(conflict)
		}
	}
}

func TestSavedNukeProjectsUseApplicationScopeAndDoNotRequireLiveDiscovery(t *testing.T) {
	root := t.TempDir()
	r := &teardown.Report{Version: 1, Scope: teardown.Scope{Application: "attestra", Account: "123456789012"}, Targets: []teardown.Target{
		{Project: "attestra-application", Backend: "s3://app-state", Region: "us-east-2", Stack: "qa", Directory: filepath.Join(root, "infra")},
		{Project: "attestra-control-plane", Backend: "s3://shared-state", Region: "us-east-2", Stack: "shared", Shared: true},
	}}
	cfg := pulumicli.Config{Scope: r.Scope}
	if err := mergeSavedNukeProjects(root, &cfg, r); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 2 || cfg.Projects[0].Directory != filepath.Join(root, "infra") || !cfg.Projects[1].Shared {
		t.Fatal(cfg)
	}
	r.Targets[0].Project = "unrelated"
	if mergeSavedNukeProjects(root, &cfg, r) == nil {
		t.Fatal("foreign project accepted")
	}
}

func TestActiveNukeBlocksOtherLocalOperatorFlows(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".attestra")
	os.MkdirAll(dir, 0700)
	if err := ensureNoActiveComposerNuke(root); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "nuke.123.local.json.lock"), []byte("pid 1"), 0600)
	if ensureNoActiveComposerNuke(root) == nil {
		t.Fatal("active nuke ignored")
	}
}

func TestArtifactOnlyNukeReportReconstructsChecksWithoutDiscovery(t *testing.T) {
	scope := teardown.Scope{Application: "attestra", Account: "123456789012"}
	r := &teardown.Report{Version: 1, Scope: scope, Artifacts: []teardown.Artifact{{ID: "owned-state", Kind: "recovery_bucket", Region: "us-east-2", Recovery: true}}}
	cfg := pulumicli.Config{Scope: scope}
	if err := mergeSavedNukeProjects(t.TempDir(), &cfg, r); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 2 || len(cfg.Recovery) != 1 {
		t.Fatal(cfg)
	}
	// An external-only report needs no Pulumi backend for its saved app checks.
	r.Artifacts = []teardown.Artifact{{ID: "dev", Kind: "attestra_github_environment"}}
	cfg = pulumicli.Config{Scope: scope}
	if err := mergeSavedNukeProjects(t.TempDir(), &cfg, r); err != nil || len(cfg.Projects) != 0 {
		t.Fatal(cfg, err)
	}
}
