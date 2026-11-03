// artifact-snapshot is a host helper, never a Lambda. Report data arrives only
// through a private bounded file; credentials use the runner's isolated SDK chain.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/lambdawalker/go.attestra.aws/artifactsnapshot"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
)

type publisher interface {
	Publish(context.Context, string, reporting.Report) (artifactsnapshot.PublishOutcome, error)
}

func readReport(env, path string) (reporting.Report, error) {
	link, e := os.Lstat(path)
	if e != nil || !link.Mode().IsRegular() {
		return reporting.Report{}, artifactsnapshot.ErrCorrupt
	}
	f, e := os.Open(path)
	if e != nil {
		return reporting.Report{}, artifactsnapshot.ErrCorrupt
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) || st.Size() > artifactsnapshot.MaxJSON {
		return reporting.Report{}, artifactsnapshot.ErrCorrupt
	}
	b, e := io.ReadAll(io.LimitReader(f, artifactsnapshot.MaxJSON+1))
	if e != nil {
		return reporting.Report{}, artifactsnapshot.ErrCorrupt
	}
	return artifactsnapshot.Parse(b, env)
}
func publishFile(ctx context.Context, p publisher, env, path string) (artifactsnapshot.PublishOutcome, error) {
	r, e := readReport(env, path)
	if e != nil {
		return "", e
	}
	return p.Publish(ctx, env, r)
}
func run() error {
	input := flag.String("input", "", "private report file")
	env := flag.String("environment", "", "selected environment")
	table := flag.String("table", "", "validated runtime table name")
	flag.Parse()
	if flag.NArg() != 0 || *input == "" || *env == "" || *table == "" {
		return errors.New("invalid snapshot arguments")
	}
	// Validate before SDK initialization or any possible credential/network call.
	r, e := readReport(*env, *input)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cfg, e := config.LoadDefaultConfig(ctx)
	if e != nil {
		return artifactsnapshot.ErrUnavailable
	}
	if _, e = cfg.Credentials.Retrieve(ctx); e != nil {
		if strings.Contains(e.Error(), "AWS credentials could not be renewed for profile") {
			return errors.New("AWS credentials could not be renewed for profile")
		}
		return artifactsnapshot.ErrUnavailable
	}
	out, e := (artifactsnapshot.Writer{DB: dynamodb.NewFromConfig(cfg), Table: *table}).Publish(ctx, *env, r)
	if e == nil {
		fmt.Println("Artifact snapshot:", out)
	}
	return e
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
