// Compose validates the application locally; deployment belongs to the Pulumi program.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	application "github.com/lambdawalker/go.attestra.aws"
	composer "github.com/lambdawalker/go.infra.composer"
	"io"
	"os"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errout io.Writer) int {
	flags := flag.NewFlagSet("compose", flag.ContinueOnError)
	flags.SetOutput(errout)
	env := flags.String("environment", "dev", "logical environment")
	table := flags.String("runtime-table", "", "application-owned shared runtime table")
	arn := flags.String("runtime-arn", "", "table ARN")
	asJSON := flags.Bool("json", false, "print secret-safe plan JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	binding := application.RuntimeBinding{TableName: *table, TableARN: *arn}
	if *table == "" && *arn == "" {
		binding = application.RuntimeBinding{TableName: "local-runtime-fixture", TableARN: "local://runtime-fixture", TestOnly: true}
	}
	app, err := application.New(binding)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	plan, err := app.Plan(composer.Environment{Name: *env})
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	if *asJSON {
		if err := json.NewEncoder(out).Encode(plan); err != nil {
			return 1
		}
	} else {
		fmt.Fprintln(out, "LOCAL COMPOSITION PLAN — no AWS resources created")
		fmt.Fprintln(out, plan)
	}
	return 0
}
