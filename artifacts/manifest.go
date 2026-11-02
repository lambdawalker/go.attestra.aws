// Package artifacts composes manifests; command source stays in its feature.
package artifacts

import (
	auth "github.com/lambdawalker/go.apexfission.aws.auth/artifacts"
	verify "github.com/lambdawalker/go.apexfission.aws.verifyemail/artifacts"
)

type Lambda = auth.Lambda

func Lambdas() []Lambda {
	out := auth.Lambdas()
	for _, a := range verify.Lambdas() {
		out = append(out, Lambda{Name: a.Name, Package: a.Package})
	}
	return out
}
