package artifactsnapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

func fixture() reporting.Report {
	now := time.Date(2026, 11, 3, 1, 2, 3, 4, time.UTC)
	return reporting.Report{SchemaVersion: 1, Scope: reporting.Scope{Application: "attestra", Environment: "dev", Stack: "dev", Project: "attestra-application", Repository: "owner/repo", Account: "123456789012", Region: "us-east-2", Backend: "s3://state"}, Metadata: reporting.Metadata{Operation: "deploy", Outcome: "failed", Revision: "abc", StartedAt: now.Add(-time.Minute), FinishedAt: now}, Artifacts: []reporting.Artifact{{Key: "urn:one", Name: "one", Type: "aws:sqs/queue:Queue", Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed, ObservedAt: now.Add(-time.Second)}}}}
}
func zip(b []byte) []byte {
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	z.Write(b)
	z.Close()
	return out.Bytes()
}
func TestCodecPreservesEvidenceAndRejectsInvalidInput(t *testing.T) {
	r := fixture()
	b, e := Encode(r, "dev")
	if e != nil {
		t.Fatal(e)
	}
	out, e := Decode(b, "dev")
	if e != nil || !out.CacheLoaded || out.Metadata != r.Metadata || out.Artifacts[0].Observation != r.Artifacts[0].Observation {
		t.Fatalf("%+v %v", out, e)
	}
	raw, _ := json.Marshal(r)
	for name, b := range map[string][]byte{"corrupt": []byte("broken"), "oversize-compressed": make([]byte, MaxCompressed+1), "oversize-decoded": zip([]byte(strings.Repeat(" ", MaxJSON+1))), "unknown": zip(bytes.Replace(raw, []byte(`"key":"urn:one"`), []byte(`"rawState":"secret","key":"urn:one"`), 1)), "trailing": zip(append(raw, []byte(` {}`)...))} {
		t.Run(name, func(t *testing.T) {
			if _, e := Decode(b, "dev"); e == nil {
				t.Fatal("accepted invalid payload")
			}
		})
	}
	for _, change := range []func(*reporting.Report){func(r *reporting.Report) { r.SchemaVersion = 2 }, func(r *reporting.Report) { r.Scope.Environment = "prod" }, func(r *reporting.Report) { r.Scope.Stack = "prod" }, func(r *reporting.Report) { r.Scope.Application = "other" }, func(r *reporting.Report) { r.Scope.Project = "attestra-control-plane" }, func(r *reporting.Report) { r.Metadata.FinishedAt = time.Time{} }, func(r *reporting.Report) { r.Metadata.Operation = "preview" }, func(r *reporting.Report) { r.Metadata.Outcome = "" }, func(r *reporting.Report) { r.Scope.Account = "" }, func(r *reporting.Report) { r.Artifacts = append(r.Artifacts, r.Artifacts[0]) }} {
		bad := fixture()
		change(&bad)
		if _, e := Encode(bad, "dev"); e == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

type fakeDB struct {
	get  *dynamodb.GetItemInput
	put  *dynamodb.PutItemInput
	item map[string]types.AttributeValue
	err  error
}

func (f *fakeDB) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.get = in
	return &dynamodb.GetItemOutput{Item: f.item}, f.err
}
func (f *fakeDB) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.put = in
	return &dynamodb.PutItemOutput{}, f.err
}
func TestStoreReadStatesAndConditionalFreshness(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{}
	reader := Reader{DB: db, Table: "runtime"}
	if _, e := reader.Read(ctx, "dev"); !errors.Is(e, ErrMissing) {
		t.Fatal(e)
	}
	if !*db.get.ConsistentRead || db.get.Key["id"].(*types.AttributeValueMemberS).Value != "ENV#dev" || db.get.Key["sk"].(*types.AttributeValueMemberS).Value != "ARTIFACT_REPORT" {
		t.Fatal(db.get)
	}
	writer := Writer{DB: db, Table: "runtime"}
	if out, e := writer.Publish(ctx, "dev", fixture()); e != nil || out != Published {
		t.Fatal(out, e)
	}
	if !strings.Contains(*db.put.ConditionExpression, "<") || db.put.ExpressionAttributeValues[":finished"].(*types.AttributeValueMemberN).Value != "1793667723000000004" {
		t.Fatal(db.put)
	}
	db.item = db.put.Item
	if _, e := reader.Read(ctx, "dev"); e != nil {
		t.Fatal(e)
	}
	db.item["finished"] = &types.AttributeValueMemberN{Value: "1"}
	if _, e := reader.Read(ctx, "dev"); !errors.Is(e, ErrCorrupt) {
		t.Fatal(e)
	}
	db.err = &types.ConditionalCheckFailedException{}
	if out, e := writer.Publish(ctx, "dev", fixture()); e != nil || out != Stale {
		t.Fatal(out, e)
	}
	db.err = errors.New("private provider diagnostics")
	if _, e := reader.Read(ctx, "dev"); !errors.Is(e, ErrUnavailable) || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	db.err = nil
	db.item = map[string]types.AttributeValue{"payload": &types.AttributeValueMemberB{Value: []byte("bad")}}
	if _, e := reader.Read(ctx, "dev"); !errors.Is(e, ErrCorrupt) {
		t.Fatal(e)
	}
}
func TestHTMLBound(t *testing.T) {
	r := fixture()
	r.Artifacts = nil
	for i := 0; i < 3500; i++ {
		r.Artifacts = append(r.Artifacts, reporting.Artifact{Key: fmt.Sprintf("artifact-%d", i), Name: "sample", Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}})
	}
	if _, e := Encode(r, "dev"); e != nil {
		t.Fatal("fixture must fit snapshot bounds", e)
	}
	if _, e := HTML(r); !errors.Is(e, ErrRenderTooLarge) {
		t.Fatal(e)
	}
}

func TestEncodingSizeLimitsNeverTruncate(t *testing.T) {
	r := fixture()
	r.Artifacts[0].Name = strings.Repeat("a", MaxJSON)
	if _, e := Encode(r, "dev"); !errors.Is(e, ErrTooLarge) {
		t.Fatal(e)
	}
	// Deterministic printable random data remains below the JSON cap but exceeds
	// the gzip cap; a single oversized item is rejected whole.
	var x uint32 = 1234567
	var b strings.Builder
	for i := 0; i < 400000; i++ {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b.WriteByte(byte('!' + x%90))
	}
	r = fixture()
	r.Artifacts[0].Name = b.String()
	if _, e := Encode(r, "dev"); !errors.Is(e, ErrTooLarge) {
		t.Fatal(e)
	}
}
