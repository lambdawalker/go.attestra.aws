package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/ui"
)

// Launcher bookkeeping does not count as a user-selected operation.
// A completion record routes the UI only; deployment still verifies live state.
func deploymentStartup(root, repo string, flags []string, choose func() (string, error)) (string, error) {
	for _, name := range flags {
		if name != "repo-root" && name != "project" && name != "deploy" && name != "timed-confirm" {
			return "explicit", nil
		}
	}
	paths, err := statePaths(stateDirectory(root), "bootstrap.*.local.json", "bootstrap.*.local.json.bak")
	if err != nil {
		return "", err
	}
	// Include dev even when only an unqualified legacy checkpoint exists.
	environments := []string{"dev"}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".bak")
		environment := strings.TrimSuffix(strings.TrimPrefix(name, "bootstrap."), ".local.json")
		if environment != "" && !slices.Contains(environments, environment) {
			environments = append(environments, environment)
		}
	}
	completed := false
	for _, environment := range environments {
		checkpoint, err := readBootstrapCheckpoint(root, environment)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if checkpoint.belongsTo(repo, environment) && (checkpoint.Environment != "" || environment == "dev") && checkpoint.Stage == "complete" {
			completed = true
		}
	}
	if !completed {
		return "setup", nil
	}
	return choose()
}

func chooseDeployment() (string, error) { return chooseDeploymentFor("") }
func chooseDeploymentFor(root string) (string, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return "", errors.New("run in an interactive terminal or specify an explicit operation")
	}
	saved, e := readSelection(root)
	if e != nil {
		return "", e
	}
	choice := saved.Scope
	if choice == "" {
		choice = "Backend"
	}
	if e = ui.Choice("What would you like to deploy? All includes only the selected backend environment", &choice, "Admin/index", "Backend", "All"); e != nil {
		return "", e
	}
	if root != "" {
		saved.Scope = choice
		if e = writeSelection(root, saved); e != nil {
			return "", e
		}
	}
	return map[string]string{"Admin/index": "shared", "Backend": "app", "All": "both"}[choice], nil
}

func runDeploymentChoice(scope string, shared, backend func() error) error {
	switch scope {
	case "shared":
		return shared()
	case "app":
		return backend()
	case "both":
		if err := shared(); err != nil {
			return fmt.Errorf("shared deployment did not complete; backend deployment was not started: %w", err)
		}
		return backend()
	default:
		return errors.New("unknown deployment choice")
	}
}
