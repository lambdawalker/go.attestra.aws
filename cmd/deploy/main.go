package main

import (
	"context"
	"fmt"
	"github.com/lambdawalker/go.apexfission.aws.auth/deploy/githubsetup"
	"github.com/lambdawalker/go.apexfission.aws.auth/deploy/wizard"
	"github.com/lambdawalker/go.attestra.aws/internal/deployment"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root, err := os.Getwd()
	if err == nil {
		err = wizard.Run(ctx, wizard.Options{GitHubPolicyProfile: githubsetup.AttestraProfile, Root: root, Input: os.Stdin, Output: os.Stdout, Args: os.Args[1:], SharedBuilds: deployment.SharedBuilds(), BackendBuilds: deployment.BackendBuilds()})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
