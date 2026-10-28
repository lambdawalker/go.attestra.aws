package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/lambdawalker/go.apexfission.aws.auth/router"
	"github.com/lambdawalker/go.attestra.aws/internal/bootstrap"
	"log"
	"os"
)

func main() {
	s, e := bootstrap.ReadSettings(os.Getenv)
	if e != nil {
		log.Fatal(e)
	}
	ctx := context.Background()
	cfg, e := config.LoadDefaultConfig(ctx)
	if e != nil {
		log.Fatal("AWS configuration unavailable")
	}
	h, e := bootstrap.Handler(ctx, cfg, s)
	if e != nil {
		log.Fatal(e)
	}
	lambda.Start(router.Lambda(h))
}
