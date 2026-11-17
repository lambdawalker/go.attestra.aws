package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func readAuthoritativeConfiguration(r commandRunner, o options, known bool) (map[string]struct{ Value string }, error) {
	dir := filepath.Join(o.Root, "infra")
	data, e := r.Exec(dir, true, "pulumi", "stack", "ls", "--json", "--non-interactive")
	if e != nil {
		return nil, errors.New("cannot discover original stack; no settings inferred from local file absence")
	}
	exists, e := stackListed(data, o.Stack)
	if e != nil {
		return nil, e
	}
	if !exists {
		paths, e := filepath.Glob(filepath.Join(dir, "Pulumi."+o.Stack+".yaml*"))
		if e != nil {
			return nil, e
		}
		if known || len(paths) > 0 {
			return nil, errors.New("previous stack/configuration is missing from backend; restore original state before setup")
		}
		return map[string]struct{ Value string }{}, nil
	}
	if _, e = r.Exec(dir, true, "pulumi", "stack", "select", o.Stack, "--non-interactive"); e != nil {
		return nil, e
	}
	if _, e = r.Exec(dir, true, "pulumi", "config", "refresh", "--stack", o.Stack, "--force", "--non-interactive"); e != nil {
		return nil, errors.New("cannot restore original encrypted configuration; no new values selected")
	}
	data, e = r.Exec(dir, true, "pulumi", "config", "--json", "--stack", o.Stack)
	if e != nil {
		return nil, e
	}
	var current map[string]struct{ Value string }
	if json.Unmarshal(data, &current) != nil || current == nil {
		return nil, errors.New("invalid authoritative configuration")
	}
	return current, nil
}
func backupStackConfiguration(root, dir string, args []string) error {
	stack := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--stack" {
			stack = args[i+1]
		}
	}
	if stack == "" {
		return errors.New("configuration restoration requires explicit stack")
	}
	if e := (options{Backend: "s3://placeholder-state", Stack: stack, Region: "us-east-2"}).validate(); e != nil {
		return e
	}
	source := filepath.Join(dir, "Pulumi."+stack+".yaml")
	sum, e := fileSHA(source)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	target := filepath.Join(root, ".deploy", "config-backups", artifactProject(dir), fmt.Sprintf("%s-%s.yaml", stack, sum))
	if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
		return e
	}
	return copyRecovery(source, target)
}
