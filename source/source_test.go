package source

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zxc0zxc0zxc/reconciliation/config"
	v1 "github.com/zxc0zxc0zxc/reconciliation/gen/reconciliation/v1"
	"github.com/zxc0zxc0zxc/reconciliation/recon"
)

type stubServer struct {
	v1.UnimplementedRecordSourceServer
	records     []*v1.Record
	pageSize    int
	asOf        time.Time
	stuckCursor bool
}

func (s *stubServer) GetRecords(_ context.Context, req *v1.GetRecordsRequest) (*v1.GetRecordsResponse, error) {
	if s.stuckCursor {
		return &v1.GetRecordsResponse{
			Records:    s.records,
			AsOf:       timestamppb.New(s.asOf),
			NextCursor: "stuck",
		}, nil
	}

	offset := 0
	if c := req.GetCursor(); c != "" {
		parsed, err := strconv.Atoi(c)
		if err != nil {
			return nil, err
		}
		offset = parsed
	}
	end := offset + s.pageSize
	if end > len(s.records) {
		end = len(s.records)
	}
	resp := &v1.GetRecordsResponse{Records: s.records[offset:end], AsOf: timestamppb.New(s.asOf)}
	if end < len(s.records) {
		resp.NextCursor = strconv.Itoa(end)
	}
	return resp, nil
}

func startServer(t *testing.T, srv *stubServer) config.Endpoint {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	v1.RegisterRecordSourceServer(server, srv)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(server.Stop)

	return config.Endpoint{
		Name:     "stub",
		Address:  listener.Addr().String(),
		Timeout:  5 * time.Second,
		PageSize: int32(srv.pageSize),
	}
}

func TestFetchPagesThroughWindow(t *testing.T) {
	asOf := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	records := make([]*v1.Record, 0, 7)
	for i := 0; i < 7; i++ {
		records = append(records, ToProto(recon.Record{
			ID:         "id-" + strconv.Itoa(i),
			Key:        "key-" + strconv.Itoa(i),
			Amount:     int64(i) * 100,
			Currency:   "USD",
			OccurredAt: asOf.Add(-time.Hour),
			Status:     "settled",
			Attributes: map[string]string{"batch": "1"},
		}))
	}
	endpoint := startServer(t, &stubServer{records: records, pageSize: 3, asOf: asOf})

	client, err := Dial(endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	snapshot, err := client.Fetch(context.Background(), recon.Window{From: asOf.Add(-24 * time.Hour), To: asOf})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(snapshot.Records) != len(records) {
		t.Fatalf("records = %d, want %d", len(snapshot.Records), len(records))
	}
	if !snapshot.AsOf.Equal(asOf) {
		t.Errorf("as-of = %s, want %s", snapshot.AsOf, asOf)
	}
	if snapshot.Source != "stub" {
		t.Errorf("source = %q, want %q", snapshot.Source, "stub")
	}
	first := snapshot.Records[0]
	if first.Key != "key-0" || first.Currency != "USD" || first.Attributes["batch"] != "1" {
		t.Errorf("first record = %+v", first)
	}
}

func TestFetchRejectsStuckCursor(t *testing.T) {
	asOf := time.Now().UTC()
	records := []*v1.Record{ToProto(recon.Record{ID: "1", Key: "k", OccurredAt: asOf})}
	endpoint := startServer(t, &stubServer{records: records, pageSize: 1, asOf: asOf, stuckCursor: true})

	client, err := Dial(endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.Fetch(context.Background(), recon.Window{From: asOf.Add(-time.Hour), To: asOf}); !errors.Is(err, ErrCursorStuck) {
		t.Fatalf("Fetch error = %v, want ErrCursorStuck", err)
	}
}
