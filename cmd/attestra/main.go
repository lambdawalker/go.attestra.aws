package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	"github.com/lambdawalker/go.apexfission.aws.auth/deploy"
	"github.com/lambdawalker/go.apexfission.aws.auth/deploy/build"
	"github.com/lambdawalker/go.attestra.aws/app"
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
		return fmt.Errorf("usage: attestra init|build|check|preview|deploy|status|teardown [--env dev]")
	}
	op := args[0]
	f := flag.NewFlagSet(op, flag.ContinueOnError)
	f.SetOutput(out)
	manifest := f.String("manifest", "apexfission.json", "application configuration")
	environment := f.String("env", "dev", "target environment")
	confirm := f.String("confirm", "", "exact destructive target")
	destroy := f.Bool("allow-destroy", false, "authorize resource destruction")
	production := f.Bool("allow-production", false, "authorize production destruction")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	switch op {
	case "init", "build", "check", "preview", "deploy", "status", "teardown":
	default:
		return fmt.Errorf("unknown operation %q", op)
	}
	if op == "init" {
		if e := project.Init(*manifest); e != nil {
			return e
		}
		fmt.Fprintln(out, "Created", *manifest, "— set the AWS account and state bucket before preview/deploy.")
		return nil
	}
	root, e := filepath.Abs(filepath.Dir(*manifest))
	if e != nil {
		return e
	}
	a, e := app.Compose(auth.Module{})
	if e != nil {
		return e
	}
	if op == "build" {
		return build.Lambdas(ctx, root, a.Builds)
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
	engine := deploy.Engine{Manifest: m, Root: root}
	if e = engine.Use(a); e != nil {
		return e
	}
	return engine.Execute(ctx, op, *environment, deploy.Options{Confirm: *confirm, AllowDestroy: *destroy, AllowProduction: *production})
}
