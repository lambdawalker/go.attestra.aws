package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/ui"
)

type projectIdentity struct {
	Version                    int
	Name, Repository, Checkout string
}

func projectPath(root string) string { return filepath.Join(root, ".deploy", "project.json") }
func readProject(root string) (projectIdentity, error) {
	var p projectIdentity
	b, e := os.ReadFile(projectPath(root))
	if e != nil {
		return p, e
	}
	if json.Unmarshal(b, &p) != nil || p.Version != 1 || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(p.Name) {
		return p, errors.New("invalid project identity; restore .deploy/project.json")
	}
	absolute, e := filepath.Abs(root)
	if e != nil {
		return p, e
	}
	if p.Checkout != absolute {
		return p, errors.New("project record belongs to another checkout; restore deliberately, do not rename vaults")
	}
	if repo := localRepository(root); repo != "" && repo != p.Repository {
		return p, errors.New("project repository mismatch")
	}
	return p, nil
}
func centralDirectory(p projectIdentity) (string, error) {
	home, e := os.UserHomeDir()
	return filepath.Join(home, ".infra-composer", p.Name), e
}
func stateDirectory(root string) string {
	p, e := readProject(root)
	if e == nil {
		if dir, e := centralDirectory(p); e == nil {
			return dir
		}
	}
	// Tests and legacy helper callers have no project identity. CLI validates it.
	return filepath.Join(root, ".deploy")
}
func atomicPrivateJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), "record-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(append(b, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func establishProject(root, name string, interactive bool) error {
	repo := localRepository(root)
	if repo == "" {
		return errors.New("project requires a GitHub origin repository")
	}
	p, e := readProject(root)
	if e == nil {
		if name != "" && name != p.Name {
			return errors.New("explicit project differs from saved identity")
		}
		return validateCentralProject(p)
	}
	if !os.IsNotExist(e) {
		return e
	}
	if name == "" {
		if !interactive {
			return errors.New("explicit -project is required without trusted saved project identity")
		}
		name = "attestra"
		if e := ui.Input("Project name (groups private deployment records)", &name, false, true).Validate(func(s string) error {
			if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(s) {
				return errors.New("use a lowercase project name, at most 48 characters")
			}
			return nil
		}).Run(); e != nil {
			return e
		}
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(name) {
		return errors.New("invalid project name")
	}
	absolute, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	p = projectIdentity{1, name, repo, absolute}
	if e = validateCentralProject(p); e != nil {
		return e
	}
	return atomicPrivateJSON(projectPath(root), p)
}
func validateCentralProject(p projectIdentity) error {
	dir, e := centralDirectory(p)
	if e != nil {
		return e
	}
	path := filepath.Join(dir, "project.json")
	b, e := os.ReadFile(path)
	if e == nil {
		var old projectIdentity
		if json.Unmarshal(b, &old) != nil || old != p {
			return fmt.Errorf("project name collision at %s; choose a different project or deliberately restore its checkout", path)
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	// Exclusive creation prevents two checkouts from claiming one name.
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(p)
}

type deploymentSelection struct {
	Version            int
	Scope, Environment string
}

func readSelection(root string) (deploymentSelection, error) {
	var s deploymentSelection
	if root == "" {
		return s, nil
	}
	b, e := os.ReadFile(filepath.Join(root, ".deploy", "selection.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if json.Unmarshal(b, &s) != nil || s.Version != 1 {
		return s, errors.New("invalid saved component/environment selection")
	}
	if s.Scope != "" && s.Scope != "Admin/index" && s.Scope != "Backend" && s.Scope != "All" {
		return s, errors.New("invalid saved component scope")
	}
	if s.Environment != "" {
		if e = ui.ValidateEnvironment(s.Environment); e != nil {
			return s, e
		}
	}
	return s, nil
}
func writeSelection(root string, s deploymentSelection) error {
	s.Version = 1
	return atomicPrivateJSON(filepath.Join(root, ".deploy", "selection.json"), s)
}
func saveSelectionEnvironment(root, environment string) error {
	s, e := readSelection(root)
	if e != nil {
		return e
	}
	s.Environment = environment
	return writeSelection(root, s)
}

func acquireOperatorLock(root string) (func(), error) {
	dir := stateDirectory(root)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "operator.lock")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, fmt.Errorf("operator run already active or interrupted at %s; inspect it, never steal an active lock: %w", path, e)
	}
	fmt.Fprintln(f, os.Getpid())
	f.Close()
	return func() { _ = os.Remove(path) }, nil
}
