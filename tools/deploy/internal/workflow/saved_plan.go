package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/ui"
)

// Plans are process-local capabilities. They cannot be recovered as approvals.
// Interrupted attempts must preview again; Pulumi still validates the saved plan
// against remote state when applying it.
type savedPlan struct {
	effectiveConcurrency                    *int
	path, directory, target, inputs, digest string
	destructive                             bool
	sealedAt                                time.Time
}

func planTarget(dir string, env, args []string) string {
	target := []string{filepath.Clean(dir)}
	for _, key := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ACCESS_KEY_ID", "AWS_SESSION_TOKEN", "PULUMI_BACKEND_URL", "PULUMI_CONFIG_PASSPHRASE"} {
		// Hash credentials as part of the in-memory binding, never persist them.
		value := sha256.Sum256([]byte(reportEnv(env, key)))
		target = append(target, key+"="+hex.EncodeToString(value[:]))
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--stack", "--target", "--exclude":
			if i+1 < len(args) {
				target = append(target, args[i], args[i+1])
				i++
			}
		case "--target-dependents", "--target-dependents=true":
			target = append(target, "--target-dependents")
		default:
			if strings.HasPrefix(args[i], "--stack=") || strings.HasPrefix(args[i], "--target=") || strings.HasPrefix(args[i], "--exclude=") {
				target = append(target, args[i])
			}
		}
	}
	b, _ := json.Marshal(target)
	return string(b)
}

func fileSHA(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("expected regular file")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Cover local source, embedded inputs, encrypted configuration and built archives.
// The pinned module graph and build cache cover external dependency content.
func planInputs(root string) (string, error) {
	h := sha256.New()
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if d.IsDir() {
			if slices.Contains([]string{".git", ".deploy", ".attestra", ".cache", "deployment-reports", "node_modules", ".pulumi"}, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot bind plan to symlink input %s", rel)
		}
		if strings.HasSuffix(rel, ".tmp") || strings.Contains(rel, ".local.json") {
			return nil
		}
		sum, e := fileSHA(path)
		if e != nil {
			return e
		}
		fmt.Fprintf(h, "%s\x00%s\n", filepath.ToSlash(rel), sum)
		return nil
	})
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newSavedPlan(root, dir string, env, args []string) (*savedPlan, error) {
	inputs, e := planInputs(root)
	if e != nil {
		return nil, e
	}
	parent := filepath.Join(root, ".deploy", "plans")
	if e = os.MkdirAll(parent, 0700); e != nil {
		return nil, e
	}
	private, e := os.MkdirTemp(parent, "attempt-")
	if e != nil {
		return nil, e
	}
	return &savedPlan{path: filepath.Join(private, "plan.json"), directory: dir, target: planTarget(dir, env, args), inputs: inputs, destructive: true}, nil
}
func (s *savedPlan) seal(events string) error {
	s.destructive = true
	digest, e := fileSHA(s.path)
	if e != nil {
		return fmt.Errorf("successful preview did not produce a saved plan: %w", e)
	}
	s.digest = digest
	s.sealedAt = time.Now()
	// Unknown/malformed event summaries require deliberate approval too.
	f, e := os.Open(events)
	if e != nil {
		return nil
	}
	defer f.Close()
	changes, e := readPreviewChanges(f)
	if e == nil && changes != nil {
		s.destructive = false
		for k, n := range changes {
			if n > 0 && (strings.Contains(k, "delete") || strings.Contains(k, "replace")) {
				s.destructive = true
			}
		}
	}
	return nil
}
func (s *savedPlan) validate(root, dir string, env, args []string) error {
	if s == nil || s.digest == "" {
		return errors.New("apply requires a successful saved preview in this run")
	}
	if time.Since(s.sealedAt) > 20*time.Minute {
		return errors.New("saved preview expired; preview again")
	}
	if s.target != planTarget(dir, env, args) {
		return errors.New("preview target or credentials changed; preview again")
	}
	sum, e := fileSHA(s.path)
	if e != nil || sum != s.digest {
		return errors.New("saved preview plan missing or changed; preview again")
	}
	inputs, e := planInputs(root)
	if e != nil {
		return e
	}
	if inputs != s.inputs {
		return errors.New("source, configuration or artifacts changed after preview; preview again")
	}
	return nil
}
func (s *savedPlan) cleanup() {
	if s != nil {
		_ = os.RemoveAll(filepath.Dir(s.path))
	}
}
func approveSavedPlan(s *savedPlan, ci bool) error {
	if ci {
		if s.destructive {
			return errors.New("CI preview includes deletion/replacement or unknown change summary; deliberate local review is required")
		}
		return nil
	}
	if s.destructive {
		return ui.Confirm("Preview contains deletion/replacement or an unknown summary. Apply this exact plan?")
	}
	return ui.Continue("Deploy the previewed changes? [Y/n]")
}

func (s *savedPlan) recordApplied(root, dir, account string, env, args []string) error {
	stack := ""
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--stack" {
			stack = args[i+1]
		}
	}
	if stack == "" {
		return errors.New("cannot record apply without explicit stack")
	}
	record := struct {
		Version                                                                                  int
		Account, Region, Backend, Project, Stack, DesiredFingerprint, PlanFingerprint, AppliedAt string
	}{1, account, reportEnv(env, "AWS_REGION"), artifactBackend(reportEnv(env, "PULUMI_BACKEND_URL")), artifactProject(dir), stack, s.inputs, s.digest, time.Now().UTC().Format(time.RFC3339)}
	name := "applied." + artifactProject(dir) + "." + stack + ".local.json"
	if e := atomicPrivateJSON(filepath.Join(stateDirectory(root), name), record); e != nil {
		return e
	}
	return atomicPrivateJSON(filepath.Join(root, ".deploy", name), record)
}
