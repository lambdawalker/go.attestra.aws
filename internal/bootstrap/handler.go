package bootstrap

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/lambdawalker/go.apexfission.aws.auth/admin"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	"github.com/lambdawalker/go.apexfission.aws.auth/authorization"
	"github.com/lambdawalker/go.apexfission.aws.auth/awsemail"
	"github.com/lambdawalker/go.apexfission.aws.auth/awsidentity"
	"github.com/lambdawalker/go.apexfission.aws.auth/awsstore"
	"github.com/lambdawalker/go.apexfission.aws.auth/config"
	"github.com/lambdawalker/go.apexfission.aws.auth/email"
	"github.com/lambdawalker/go.apexfission.aws.auth/passkey"
	"github.com/lambdawalker/go.apexfission.aws.auth/signin"
	"github.com/lambdawalker/go.attestra.aws/app"
	"net/http"
	"net/url"
)

// Handler builds either the customer API or the dedicated administrator shell.
// AWS clients are lazy; constructing this handler does not provision resources.
func Handler(_ context.Context, cfg aws.Config, s Settings) (http.Handler, error) {
	keys, e := auth.NewJWKS(s.Issuer)
	if e != nil {
		return nil, e
	}
	verifier := auth.Verifier{Issuer: s.Issuer, ClientID: s.ClientID, Keys: keys}
	db := dynamodb.NewFromConfig(cfg)
	module := auth.Module{}
	if s.Mode == "auth" {
		id := cognitoidentityprovider.NewFromConfig(cfg)
		issuer, e := url.Parse(s.Issuer)
		if e != nil || issuer.Scheme != "https" || issuer.Host == "" {
			return nil, fmt.Errorf("invalid issuer")
		}
		module.Email = &email.Service{Store: awsstore.Store{DB: db, Table: s.Table}, Identity: awsidentity.Identity{Cognito: id, DB: db, Table: s.Table, PoolID: s.Pool, ClientID: s.ClientID}, Sender: awsemail.Sender{Client: sesv2.NewFromConfig(cfg), From: s.Sender}, Key: s.ProofKey, Origin: s.Origin}
		module.Passkeys = passkey.Service{Cognito: id, Endpoint: "https://" + issuer.Host + "/"}
		module.Signin = signin.Service{Client: id, ClientID: s.ClientID}
	}
	a, e := app.Compose(module)
	if e != nil {
		return nil, e
	}
	if s.Mode == "admin" {
		shell, e := a.Admin(verifier, authorization.DynamoGrants{DB: db, Table: s.ConfigTable}, []string{s.Environment}, config.Dynamo{DB: db, Table: s.ConfigTable})
		if e != nil {
			return nil, e
		}
		shell.Login = &admin.Login{Domain: s.LoginDomain, ClientID: s.ClientID, RedirectURI: s.AdminOrigin + "/admin"}
		return managedLoginBrowser(shell, s.LoginDomain)
	}
	return a.Handler(verifier, app.ProfileAuthorizer{}, s.Environment)
}
