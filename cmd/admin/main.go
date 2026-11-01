package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/lambdawalker/go.apexfission.aws.auth/runtimeconfig"
	"github.com/lambdawalker/go.attestra.aws/adminapi"
	"github.com/lambdawalker/go.attestra.aws/internal/awshttp"
	"log"
	"os"
)

func main() {
	cfg, e := config.LoadDefaultConfig(context.Background())
	if e != nil {
		log.Fatal(e)
	}
	h := adminapi.Handler{Service: runtimeconfig.Service{Store: runtimeconfig.Store{DB: dynamodb.NewFromConfig(cfg), Table: os.Getenv("RUNTIME_TABLE")}}, Issuer: os.Getenv("ADMIN_ISSUER"), ClientID: os.Getenv("ADMIN_CLIENT_ID"), Origin: os.Getenv("ADMIN_ORIGIN"), LoginDomain: os.Getenv("ADMIN_LOGIN_DOMAIN")}
	h.Reset = adminapi.LambdaResetClient{HTTP: awshttp.Client{Config: cfg}, FunctionPrefix: os.Getenv("RESET_FUNCTION_PREFIX")}
	if _, e := adminapi.ResetFunctionName(os.Getenv("RESET_FUNCTION_PREFIX"), "configuration-check"); e != nil {
		log.Fatal("invalid RESET_FUNCTION_PREFIX")
	}
	h.Passkeys = &adminapi.PasskeyMFA{Provider: cognitoidentityprovider.NewFromConfig(cfg), LoginDomain: h.LoginDomain}
	if h.Issuer == "" || h.ClientID == "" || h.Origin == "" || h.LoginDomain == "" {
		log.Fatal("missing admin configuration")
	}
	lambda.Start(h.Handle)
}
