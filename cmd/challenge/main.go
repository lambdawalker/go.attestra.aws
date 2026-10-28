package main

import (
	"context"
	"encoding/json"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/lambdawalker/go.apexfission.aws.auth/challenge"
	"log"
	"os"
)

func main() {
	table := os.Getenv("TABLE_NAME")
	if table == "" {
		log.Fatal("TABLE_NAME is required")
	}
	cfg, e := config.LoadDefaultConfig(context.Background())
	if e != nil {
		log.Fatal("AWS configuration unavailable")
	}
	h := challenge.Handler{DB: dynamodb.NewFromConfig(cfg), Table: table}
	lambda.Start(func(ctx context.Context, raw json.RawMessage) (challenge.Event, error) { return h.Handle(ctx, raw) })
}
