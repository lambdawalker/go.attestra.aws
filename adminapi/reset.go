package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lambdawalker/go.apexfission.aws.auth/envreset"
	"github.com/lambdawalker/go.apexfission.aws.auth/runtimeconfig"
	"github.com/lambdawalker/go.attestra.aws/internal/awshttp"
	"net/url"
	"regexp"
)

type ResetClient interface {
	Invoke(context.Context, envreset.Request) (envreset.Status, error)
}

// ResetFunctionPrefix is shared by application deployment and admin invocation.
// The environment suffix is always derived from an authorized runtime grant.
const ResetFunctionPrefix = "attestra-app-auth-reset-"

var resetPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,31}-$`)

// ResetFunctionName accepts only a bounded deployment-owned prefix, never an ARN
// or a browser-supplied target. The 32-byte prefix limit permits every valid
// environment within Lambda's 64-byte function-name limit.
func ResetFunctionName(prefix, environment string) (string, error) {
	if prefix == "" {
		prefix = ResetFunctionPrefix
	}
	if !resetPrefixPattern.MatchString(prefix) || !runtimeconfig.ValidEnvironment(environment) {
		return "", runtimeconfig.ErrInvalid
	}
	return prefix + environment, nil
}

type LambdaResetClient struct {
	HTTP           awshttp.Client
	FunctionPrefix string
}

func (c LambdaResetClient) Invoke(ctx context.Context, q envreset.Request) (envreset.Status, error) {
	functionName, e := ResetFunctionName(c.FunctionPrefix, q.Environment)
	if e != nil {
		return envreset.Status{}, e
	}
	body, e := json.Marshal(q)
	if e != nil {
		return envreset.Status{}, e
	}
	target := "https://lambda." + c.HTTP.Config.Region + ".amazonaws.com/2015-03-31/functions/" + url.PathEscape(functionName) + "/invocations"
	status, data, e := c.HTTP.Do(ctx, "lambda", "POST", target, body, map[string]string{"Content-Type": "application/json", "X-Amz-Invocation-Type": "RequestResponse"})
	if e != nil || status != 200 {
		return envreset.Status{}, errors.New("reset_unavailable")
	}
	var reply struct {
		Status *envreset.Status `json:"status"`
		Error  string           `json:"error"`
	}
	if json.Unmarshal(data, &reply) != nil {
		return envreset.Status{}, errors.New("reset_unavailable")
	}
	switch reply.Error {
	case "reset_disabled":
		return envreset.Status{}, runtimeconfig.ErrForbidden
	case "invalid_reset_request":
		return envreset.Status{}, runtimeconfig.ErrInvalid
	case "reset_in_progress":
		return envreset.Status{}, runtimeconfig.ErrConflict
	}
	if reply.Error != "" || reply.Status == nil || reply.Status.Environment != q.Environment {
		return envreset.Status{}, errors.New("reset_unavailable")
	}
	return *reply.Status, nil
}
