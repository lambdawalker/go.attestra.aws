package workflow

import (
	"errors"
	"fmt"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/ui"
	credentialvault "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/vault"
	"os"
)

func credentialLocation(root, repo, environment string) (string, string, error) {
	p, e := readProject(root)
	project := "attestra"
	if e == nil {
		project = p.Name
	} else if !os.IsNotExist(e) {
		return "", "", e
	}
	path, scope, e := credentialvault.LocalLocation(root, project, repo, environment)
	if e != nil {
		return "", "", e
	}
	if _, e = os.Lstat(path); e == nil {
		return path, scope, nil
	} else if !os.IsNotExist(e) {
		return "", "", e
	}
	old, _, e := credentialvault.Location(repo, environment)
	if e != nil {
		return "", "", e
	}
	if _, e = os.Lstat(old); e == nil {
		return "", "", errors.New("legacy encrypted vault found; run deploy -migrate-vault -stack <environment> to decrypt/re-encrypt it explicitly; do not rename it or create replacement secrets")
	} else if !os.IsNotExist(e) {
		return "", "", e
	}
	return path, scope, nil
}
func migrateCredentialVault(root, environment string) error {
	release, e := acquireOperatorLock(root)
	if e != nil {
		return e
	}
	defer release()
	p, e := readProject(root)
	if e != nil {
		return e
	}
	old, oldScope, e := credentialvault.Location(p.Repository, environment)
	if e != nil {
		return e
	}
	path, scope, e := credentialvault.LocalLocation(root, p.Name, p.Repository, environment)
	if e != nil {
		return e
	}
	fmt.Printf("Migrate vault for project %s / repository %s / environment %s. The original encrypted file remains a recovery copy.\n", p.Name, p.Repository, environment)
	if e = ui.Confirm("Decrypt and re-encrypt this vault into .deploy/?"); e != nil {
		return e
	}
	phrase := ""
	if e = ui.Input("Existing vault passphrase", &phrase, true, true).Run(); e != nil {
		return e
	}
	return credentialvault.Migrate(old, oldScope, path, scope, phrase)
}

// Teardown may deliberately delete a legacy vault in its original authenticated
// scope. This does not load it as a new project's vault or rename its file.
func teardownCredentialLocation(root, repo, environment string) (string, string, error) {
	path, scope, e := credentialLocation(root, repo, environment)
	if e == nil {
		return path, scope, nil
	}
	old, oldScope, oldErr := credentialvault.Location(repo, environment)
	if oldErr != nil {
		return "", "", oldErr
	}
	if _, oldErr = os.Stat(old); oldErr == nil {
		return old, oldScope, nil
	}
	return "", "", e
}
