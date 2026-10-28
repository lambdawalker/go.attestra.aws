package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/lambdawalker/go.apexfission.aws.auth/deploy/build"
	"github.com/lambdawalker/go.attestra.aws/internal/deployment"
	"github.com/lambdawalker/go.attestra.aws/internal/project"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if e := run(context.Background(), os.Args[1:], os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attestra init|build|check (local only); use deploy.sh / deploy.bat for cloud operations")
	}
	op := args[0]
	f := flag.NewFlagSet(op, flag.ContinueOnError)
	f.SetOutput(out)
	manifest := f.String("manifest", "apexfission.json", "application configuration")
	environment := f.String("env", "dev", "target environment")
	_ = f.String("confirm", "", "exact destructive target")
	_ = f.Bool("allow-destroy", false, "authorize resource destruction")
	_ = f.Bool("allow-production", false, "authorize production destruction")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	switch op {
	case "preview", "deploy", "status", "teardown":
		return fmt.Errorf("cloud operations moved to deploy.sh / deploy.bat (go run ./cmd/deploy); legacy manifests are not deployment targets")
	case "init", "build", "check":
	default:
		return fmt.Errorf("unknown operation %q", op)
	}
	if op == "init" {
		if e := project.Init(*manifest); e != nil {
			return e
		}
		fmt.Fprintln(out, "Created", *manifest, "— demonstration manifest only; use deploy.sh / deploy.bat for infrastructure.")
		return nil
	}
	root, e := filepath.Abs(filepath.Dir(*manifest))
	if e != nil {
		return e
	}
	if op == "build" {
		targets := append(deployment.SharedBuilds(), deployment.BackendBuilds()...)
		return build.Lambdas(ctx, root, targets)
	}
	m, e := project.Load(*manifest)
	if e != nil {
		return e
	}
	env, ok := m.Environments[*environment]
	if !ok {
		return fmt.Errorf("unknown environment %s", *environment)
	}
	if env.Account == "000000000000" || strings.Contains(env.Backend, "replace-") {
		return fmt.Errorf("configure the real AWS account and state bucket in %s first", *manifest)
	}
	if op == "check" {
		fmt.Fprintln(out, "Manifest and linked modules validated; no AWS calls performed.")
		return nil
	}
	return fmt.Errorf("cloud operations moved to deploy.sh / deploy.bat (go run ./cmd/deploy); legacy manifests are not deployment targets")
}
