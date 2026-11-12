package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/lambdawalker/go.apexfission.aws.auth/assetlinks"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/ui"
)

// Discovery examines directory entries only. It never opens signing secrets or
// parses exports from projects the operator has not selected.
func discoverAssociationFiles(home string) ([]string, error) {
	root := filepath.Join(home, ".projects")
	projects, err := associationDirectory(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		secrets := filepath.Join(root, project.Name(), "secrets")
		files, err := associationDirectory(secrets)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".associations.json") {
				paths = append(paths, filepath.Join(secrets, file.Name()))
			}
		}
	}
	return paths, nil
}

func associationDirectory(path string) ([]os.DirEntry, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	return os.ReadDir(path)
}

func readAssociationFile(path string) ([]assetlinks.Application, error) {
	const limit = 1 << 20
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("association export must be a regular file")
	}
	if info.Size() > limit {
		return nil, errors.New("association export exceeds 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("association export changed while opening it; select it again")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("association export exceeds 1 MiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var apps []assetlinks.Application
	if err := decoder.Decode(&apps); err != nil {
		return nil, fmt.Errorf("invalid public association JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("association export must contain one JSON array")
	}
	if len(apps) == 0 {
		return nil, errors.New("association export contains no Android applications")
	}
	if _, err := assetlinks.Render(apps); err != nil {
		return nil, err
	}
	return apps, nil
}

func chooseAssociationFile(home, environment string, paths []string) (string, error) {
	if len(paths) > 0 {
		choices := make([]huh.Option[int], 0, len(paths)+2)
		for i, path := range paths {
			label, err := filepath.Rel(filepath.Join(home, ".projects"), path)
			if err != nil {
				label = path
			}
			choices = append(choices, huh.NewOption(label, i))
		}
		choices = append(choices, huh.NewOption("Enter another JSON file path", -1), huh.NewOption("Skip; keep current approvals", -2))
		selected := 0
		if err := huh.NewSelect[int]().Title("Android signing export for " + environment).
			Description("Select one project/signing export for this backend environment. The signing environment can have a different name.").
			Options(choices...).Value(&selected).Run(); err != nil {
			return "", err
		}
		if selected == -2 {
			return "", nil
		}
		if selected >= 0 {
			return paths[selected], nil
		}
	}
	path := ""
	if err := ui.Input("Public Android association JSON (blank to skip)", &path, false, false).
		Description("Use the CLI's *.associations.json export. Custom signing state/vault locations can be entered here. See docs/android-associations.md.").Run(); err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		path = filepath.Join(home, path[2:])
	}
	return path, nil
}

func showAssociationFile(path, environment string, apps []assetlinks.Application) {
	fmt.Printf("Local Android associations • backend environment %s\nSource: %s\n", environment, path)
	for _, app := range apps {
		fmt.Println("Package:", app.PackageName)
		for _, fingerprint := range app.Fingerprints {
			fmt.Println("  SHA-256:", strings.ToUpper(fingerprint))
		}
	}
}

func confirmAssociationImport(environment string) (bool, error) {
	approve := false
	err := huh.NewConfirm().Title("Approve these public signing identities for " + environment + "?").
		Description("Adds fingerprints without removing existing approvals. Enables CloudFront hosting on appOrigin; do not approve if that hostname already serves an unrelated website.").
		Value(&approve).Run()
	return approve, err
}
