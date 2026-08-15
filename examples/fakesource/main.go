// Command fakesource serves a deterministic record set over the RecordSource API.
// Flags inject the anomalies a reconciliation is supposed to catch.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/zxc0zxc0zxc/reconciliation/gen/reconciliation/v1"
	"github.com/zxc0zxc0zxc/reconciliation/recon"
	"github.com/zxc0zxc0zxc/reconciliation/source"
)

type options struct {
	addr      string
	seed      int64
	records   int
	spacing   time.Duration
	drop      int
	shift     int
	restate   int
	duplicate int
}

func main() {
	var opts options
	flag.StringVar(&opts.addr, "addr", ":9101", "listen address")
	flag.Int64Var(&opts.seed, "seed", 1, "seed of the shared record set")
	flag.IntVar(&opts.records, "records", 200, "number of records to serve")
	flag.DurationVar(&opts.spacing, "spacing", time.Minute, "time between consecutive records")
	flag.IntVar(&opts.drop, "drop", 0, "omit every Nth record")
	flag.IntVar(&opts.shift, "shift", 0, "change the amount of every Nth record")
	flag.IntVar(&opts.restate, "restate", 0, "report every Nth record as pending")
	flag.IntVar(&opts.duplicate, "duplicate", 0, "emit every Nth record twice")
	flag.Parse()

	if err := serve(opts); err != nil {
		log.Fatalf("fakesource: %v", err)
	}
}

func serve(opts options) error {
	listener, err := net.Listen("tcp", opts.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", opts.addr, err)
	}

	server := grpc.NewServer()
	v1.RegisterRecordSourceServer(server, &recordSource{opts: opts})
	healthpb.RegisterHealthServer(server, health.NewServer())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()

	log.Printf("fakesource listening on %s, %d records, seed %d", opts.addr, opts.records, opts.seed)
	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

type recordSource struct {
	v1.UnimplementedRecordSourceServer
	opts options
}

const maxPageSize = 1000

func (s *recordSource) GetRecords(_ context.Context, req *v1.GetRecordsRequest) (*v1.GetRecordsResponse, error) {
	if req.GetFrom() == nil || req.GetTo() == nil {
		return nil, status.Error(codes.InvalidArgument, "from and to are required")
	}
	from, to := req.GetFrom().AsTime(), req.GetTo().AsTime()
	if !from.Before(to) {
		return nil, status.Error(codes.InvalidArgument, "from must precede to")
	}
	offset, err := decodeCursor(req.GetCursor())
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	all := generate(s.opts, now)

	window := make([]recon.Record, 0, len(all))
	for _, r := range all {
		if r.OccurredAt.Before(from) || !r.OccurredAt.Before(to) {
			continue
		}
		window = append(window, r)
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	if offset > len(window) {
		offset = len(window)
	}
	end := offset + pageSize
	if end > len(window) {
		end = len(window)
	}

	resp := &v1.GetRecordsResponse{AsOf: timestamppb.New(now)}
	for _, r := range window[offset:end] {
		resp.Records = append(resp.Records, source.ToProto(r))
	}
	if end < len(window) {
		resp.NextCursor = strconv.Itoa(end)
	}
	return resp, nil
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, status.Errorf(codes.InvalidArgument, "malformed cursor %q", cursor)
	}
	return offset, nil
}

// generate builds the same record set for a given seed, then applies the anomalies.
// Two servers, one seed, different flags: that is the disagreement to be found.
func generate(opts options, now time.Time) []recon.Record {
	rng := rand.New(rand.NewSource(opts.seed))
	currencies := []string{"USD", "EUR"}

	records := make([]recon.Record, 0, opts.records)
	for i := 0; i < opts.records; i++ {
		key := fmt.Sprintf("op-%05d", i)
		record := recon.Record{
			ID:         fmt.Sprintf("%s-%d", key, opts.seed),
			Key:        key,
			Amount:     int64(rng.Intn(500_000) + 100),
			Currency:   currencies[rng.Intn(len(currencies))],
			OccurredAt: now.Add(-time.Duration(opts.records-i) * opts.spacing),
			Status:     "settled",
		}

		if every(opts.drop, i) {
			continue
		}
		if every(opts.shift, i) {
			record.Amount += 1_337
		}
		if every(opts.restate, i) {
			record.Status = "pending"
		}
		records = append(records, record)
		if every(opts.duplicate, i) {
			duplicated := record
			duplicated.ID += "-dup"
			records = append(records, duplicated)
		}
	}
	return records
}

func every(n, i int) bool {
	return n > 0 && i%n == 0
}
