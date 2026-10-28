package fkqdrant

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// Connect creates a client and checks that the server answers. cfg is the
// go-client config (Host, Port, APIKey, UseTLS, ...); nil connects to
// localhost:6334 without TLS. Close the client when done.
//
// Connect uses the gRPC port (6334 by default), not the REST port 6333.
func Connect(ctx context.Context, cfg *qdrant.Config) (*qdrant.Client, error) {
	if cfg == nil {
		cfg = &qdrant.Config{}
	}
	client, err := qdrant.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if _, err := client.HealthCheck(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return client, nil
}
