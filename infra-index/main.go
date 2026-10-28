package main

import (
 "github.com/lambdawalker/go.apexfission.aws.auth/pulumi/shared"
 "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() { pulumi.Run(shared.Deploy) }
