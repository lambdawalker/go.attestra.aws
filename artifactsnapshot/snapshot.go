// Package artifactsnapshot stores only typed, bounded deployment reports.
// It deliberately separates dashboard GetItem access from deployment PutItem access.
package artifactsnapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

const (
	MaxCompressed = 256 * 1024
	MaxJSON       = 512 * 1024
	MaxHTML       = 2 * 1024 * 1024
)

var (
	ErrMissing        = errors.New("missing_snapshot")
	ErrUnavailable    = errors.New("snapshot_unavailable")
	ErrCorrupt        = errors.New("corrupt_snapshot")
	ErrTooLarge       = errors.New("snapshot_too_large")
	ErrRenderTooLarge = errors.New("report_too_large")
)
var environmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)

func valid(r reporting.Report, env string) bool {
	s, m := r.Scope, r.Metadata
	if !environmentPattern.MatchString(env) || r.SchemaVersion != reporting.SchemaVersion || s.Environment != env || s.Stack != env || s.Project != "attestra-application" || s.Application != "attestra" || s.Repository == "" || s.Account == "" || s.Region == "" || s.Backend == "" || m.Operation != "deploy" || m.StartedAt.IsZero() || m.FinishedAt.IsZero() || m.FinishedAt.Before(m.StartedAt) || m.FinishedAt.UnixNano() <= 0 {
		return false
	}
	switch m.Outcome {
	case "succeeded", "failed", "infrastructure_succeeded_followup_failed":
	default:
		return false
	}
	seen := map[string]bool{}
	for _, a := range r.Artifacts {
		if a.Key == "" || seen[a.Key] {
			return false
		}
		seen[a.Key] = true
	}
	return true
}

// Parse validates the original version/scope and unknown fields before normalization.
func Parse(b []byte, env string) (reporting.Report, error) {
	if len(b) > MaxJSON {
		return reporting.Report{}, ErrTooLarge
	}
	var r reporting.Report
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || !valid(r, env) {
		return reporting.Report{}, ErrCorrupt
	}
	return reporting.Normalize(r), nil
}
func Encode(r reporting.Report, env string) ([]byte, error) {
	if !valid(r, env) {
		return nil, ErrCorrupt
	}
	r = reporting.Normalize(r)
	b, e := json.Marshal(r)
	if e != nil {
		return nil, ErrCorrupt
	}
	if len(b) > MaxJSON {
		return nil, ErrTooLarge
	}
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	if _, e = z.Write(b); e != nil {
		return nil, ErrCorrupt
	}
	if z.Close() != nil {
		return nil, ErrCorrupt
	}
	if out.Len() > MaxCompressed {
		return nil, ErrTooLarge
	}
	return out.Bytes(), nil
}
func Decode(payload []byte, env string) (reporting.Report, error) {
	if len(payload) > MaxCompressed {
		return reporting.Report{}, ErrTooLarge
	}
	z, e := gzip.NewReader(bytes.NewReader(payload))
	if e != nil {
		return reporting.Report{}, ErrCorrupt
	}
	defer z.Close()
	b, e := io.ReadAll(io.LimitReader(z, MaxJSON+1))
	if e != nil {
		return reporting.Report{}, ErrCorrupt
	}
	r, e := Parse(b, env)
	if e != nil {
		return reporting.Report{}, e
	}
	r.CacheLoaded = true
	return r, nil
}

type Getter interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}
type Putter interface {
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}
type Reader struct {
	DB    Getter
	Table string
}
type Writer struct {
	DB    Putter
	Table string
}

func key(env string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "ENV#" + env}, "sk": &types.AttributeValueMemberS{Value: "ARTIFACT_REPORT"}}
}
func (r Reader) Read(ctx context.Context, env string) (reporting.Report, error) {
	if r.DB == nil || r.Table == "" || !environmentPattern.MatchString(env) {
		return reporting.Report{}, ErrUnavailable
	}
	out, e := r.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(r.Table), Key: key(env), ConsistentRead: aws.Bool(true)})
	if e != nil || out == nil {
		return reporting.Report{}, ErrUnavailable
	}
	if len(out.Item) == 0 {
		return reporting.Report{}, ErrMissing
	}
	payload, ok := out.Item["payload"].(*types.AttributeValueMemberB)
	if !ok {
		return reporting.Report{}, ErrCorrupt
	}
	report, e := Decode(payload.Value, env)
	if e != nil {
		return reporting.Report{}, ErrCorrupt
	}
	finished, ok := out.Item["finished"].(*types.AttributeValueMemberN)
	if !ok || finished.Value != strconv.FormatInt(report.Metadata.FinishedAt.UnixNano(), 10) {
		return reporting.Report{}, ErrCorrupt
	}
	return report, nil
}

type PublishOutcome string

const (
	Published PublishOutcome = "published"
	Stale     PublishOutcome = "stale"
)

func (w Writer) Publish(ctx context.Context, env string, r reporting.Report) (PublishOutcome, error) {
	b, e := Encode(r, env)
	if e != nil {
		return "", e
	}
	if w.DB == nil || w.Table == "" {
		return "", ErrUnavailable
	}
	finished := &types.AttributeValueMemberN{Value: strconv.FormatInt(r.Metadata.FinishedAt.UnixNano(), 10)}
	item := key(env)
	item["payload"] = &types.AttributeValueMemberB{Value: b}
	item["finished"] = finished
	_, e = w.DB.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(w.Table), Item: item, ConditionExpression: aws.String("attribute_not_exists(#finished) OR #finished < :finished"), ExpressionAttributeNames: map[string]string{"#finished": "finished"}, ExpressionAttributeValues: map[string]types.AttributeValue{":finished": finished}})
	var stale *types.ConditionalCheckFailedException
	if errors.As(e, &stale) {
		return Stale, nil
	}
	if e != nil {
		return "", ErrUnavailable
	}
	return Published, nil
}

// cappedWriter bounds allocation even when template escaping expands typed JSON.
type cappedWriter struct{ bytes.Buffer }

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.Len()+len(p) > MaxHTML {
		return 0, ErrRenderTooLarge
	}
	return w.Buffer.Write(p)
}
func HTML(r reporting.Report) (string, error) {
	w := &cappedWriter{}
	if e := reporting.WriteHTML(w, r); e != nil {
		if errors.Is(e, ErrRenderTooLarge) {
			return "", ErrRenderTooLarge
		}
		return "", ErrUnavailable
	}
	return w.String(), nil
}
