package fkqdrant_test

import (
	"context"
	"fmt"
	"log"

	"github.com/fares1978/fkqdrant"
	"github.com/qdrant/go-client/qdrant"
)

type Chunk struct {
	BotID string `json:"bot_id"`
	DocID string `json:"doc_id"`
	Text  string `json:"text"`
}

func Example() {
	ctx := context.Background()
	client, err := fkqdrant.Connect(ctx, &qdrant.Config{Host: "localhost", Port: 6334})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	chunks := fkqdrant.NewCollection[Chunk](client, "chunks", "chunk")
	if err := chunks.EnsureCollection(ctx, 3, qdrant.Distance_Cosine); err != nil {
		log.Fatal(err)
	}
	if err := chunks.EnsureTenantIndex(ctx, "bot_id"); err != nil {
		log.Fatal(err)
	}

	err = chunks.Upsert(ctx, fkqdrant.Point[Chunk]{
		ID:      fkqdrant.IDFrom("doc1:0"),
		Vector:  []float32{0.1, 0.9, 0.2},
		Payload: Chunk{BotID: "bot1", DocID: "doc1", Text: "Opening hours: 9-18"},
	})
	if err != nil {
		log.Fatal(err)
	}

	hits, err := chunks.Search(ctx, []float32{0.1, 0.8, 0.3}, fkqdrant.SearchOpts{
		Limit:  5,
		Filter: fkqdrant.Must(fkqdrant.Eq("bot_id", "bot1")),
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, h := range hits {
		fmt.Printf("%.2f %s\n", h.Score, h.Payload.Text)
	}

	_ = fkqdrant.Must(qdrant.NewRange("price", &qdrant.Range{Lte: qdrant.PtrOf(100.0)}))
}
