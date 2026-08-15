// Package source fetches record snapshots from RecordSource servers and
// converts them into the engine model.
package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zxc0zxc0zxc/reconciliation/config"
	v1 "github.com/zxc0zxc0zxc/reconciliation/gen/reconciliation/v1"
	"github.com/zxc0zxc0zxc/reconciliation/recon"
)

// Snapshot is one side of a comparison, taken at a single instant.
type Snapshot struct {
	Source  string
	Records []recon.Record
	AsOf    time.Time
}

// Fetcher returns the records a source holds for a window.
type Fetcher interface {
	Name() string
	Fetch(ctx context.Context, window recon.Window) (Snapshot, error)
}

// Client is a Fetcher backed by a RecordSource gRPC server.
type Client struct {
	endpoint config.Endpoint
	conn     *grpc.ClientConn
	rpc      v1.RecordSourceClient
}

// Dial connects to the endpoint. The caller owns the returned client and must Close it.
func Dial(endpoint config.Endpoint) (*Client, error) {
	creds := insecure.NewCredentials()
	if endpoint.TLS {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("system cert pool: %w", err)
		}
		creds = credentials.NewTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
	}
	conn, err := grpc.NewClient(endpoint.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial %s (%s): %w", endpoint.Name, endpoint.Address, err)
	}
	return &Client{endpoint: endpoint, conn: conn, rpc: v1.NewRecordSourceClient(conn)}, nil
}

// Name returns the configured source name.
func (c *Client) Name() string { return c.endpoint.Name }

// Close releases the connection.
func (c *Client) Close() error { return c.conn.Close() }

// ErrCursorStuck reports a server that keeps returning the same page cursor.
var ErrCursorStuck = errors.New("source did not advance the cursor")

// Fetch pages through the window and returns everything the source holds for it.
func (c *Client) Fetch(ctx context.Context, window recon.Window) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, c.endpoint.Timeout)
	defer cancel()

	snapshot := Snapshot{Source: c.endpoint.Name}
	var cursor string
	for {
		resp, err := c.rpc.GetRecords(ctx, &v1.GetRecordsRequest{
			From:     timestamppb.New(window.From),
			To:       timestamppb.New(window.To),
			Cursor:   cursor,
			PageSize: c.endpoint.PageSize,
		})
		if err != nil {
			return Snapshot{}, fmt.Errorf("collect %s: %w", c.endpoint.Name, err)
		}
		for _, r := range resp.GetRecords() {
			snapshot.Records = append(snapshot.Records, fromProto(r))
		}
		if snapshot.AsOf.IsZero() {
			snapshot.AsOf = resp.GetAsOf().AsTime()
		}
		next := resp.GetNextCursor()
		if next == "" {
			return snapshot, nil
		}
		if next == cursor {
			return Snapshot{}, fmt.Errorf("collect %s: %w", c.endpoint.Name, ErrCursorStuck)
		}
		cursor = next
	}
}

func fromProto(r *v1.Record) recon.Record {
	return recon.Record{
		ID:         r.GetId(),
		Key:        r.GetKey(),
		Amount:     r.GetAmount(),
		Currency:   r.GetCurrency(),
		OccurredAt: r.GetOccurredAt().AsTime(),
		Status:     r.GetStatus(),
		Attributes: r.GetAttributes(),
	}
}

// ToProto converts an engine record into the wire model, for servers answering GetRecords.
func ToProto(r recon.Record) *v1.Record {
	return &v1.Record{
		Id:         r.ID,
		Key:        r.Key,
		Amount:     r.Amount,
		Currency:   r.Currency,
		OccurredAt: timestamppb.New(r.OccurredAt),
		Status:     r.Status,
		Attributes: r.Attributes,
	}
}
