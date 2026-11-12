module github.com/lambdawalker/go.attestra.aws

go 1.27.3

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.20.16 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.46.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.13.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.42.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.76.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.12.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.39.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.44.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.9 // indirect
	github.com/aws/smithy-go v1.28.8 // indirect
)

tool (
	github.com/lambdawalker/go.apexfission.aws.auth/cmd/app-config
	github.com/lambdawalker/go.apexfission.aws.auth/cmd/challenge
	github.com/lambdawalker/go.apexfission.aws.auth/cmd/environment-reset
	github.com/lambdawalker/go.apexfission.aws.auth/cmd/passkeycomplete
	github.com/lambdawalker/go.apexfission.aws.auth/cmd/passkeyoptions
	github.com/lambdawalker/go.apexfission.aws.auth/cmd/signin
	github.com/lambdawalker/go.apexfission.aws.verifyemail/cmd/confirm
	github.com/lambdawalker/go.apexfission.aws.verifyemail/cmd/resend
	github.com/lambdawalker/go.apexfission.aws.verifyemail/cmd/signup
	github.com/lambdawalker/go.appexfission.aws.card-capture/cmd/capture
	github.com/lambdawalker/go.appexfission.aws.card-capture/cmd/capture-dispatcher
	github.com/lambdawalker/go.appexfission.aws.card-capture/cmd/capture-worker
)

require (
	github.com/aws/aws-lambda-go v1.55.1
	github.com/aws/aws-sdk-go-v2 v1.47.8
	github.com/aws/aws-sdk-go-v2/config v1.33.16
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.21.20
	github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider v1.75.10
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.73.2
	github.com/lambdawalker/go.apexfission.aws.auth v0.0.0-20261107054140-3c536bf9cbd8
	github.com/lambdawalker/go.apexfission.aws.verifyemail v0.0.0-20261103183508-9f175d1d1aa4
	github.com/lambdawalker/go.appexfission.aws.card-capture v0.0.0-20261112175402-aa6afcf54121
	github.com/lambdawalker/go.infra.composer v0.0.0-20261103235125-ad4cfd268616
)
