// Package fkqdrant is a small generic layer over the official Qdrant Go
// client (github.com/qdrant/go-client).
//
// A Collection[T] stores points whose payload is a Go struct T (with json
// tags), so callers work with their own types instead of qdrant.Value maps:
//
//	type Chunk struct {
//		BotID string `json:"bot_id"`
//		DocID string `json:"doc_id"`
//		Text  string `json:"text"`
//	}
//
//	chunks := fkqdrant.NewCollection[Chunk](client, "chunks", "chunk")
//	err := chunks.EnsureCollection(ctx, 1024, qdrant.Distance_Cosine)
//	err = chunks.Upsert(ctx, fkqdrant.Point[Chunk]{ID: fkqdrant.NewID(), Vector: vec, Payload: c})
//	hits, err := chunks.Search(ctx, queryVec, fkqdrant.SearchOpts{
//		Limit:  5,
//		Filter: fkqdrant.Must(fkqdrant.Eq("bot_id", botID)),
//	})
//
// The package only stores and searches vectors; producing embeddings is up
// to the caller. Use Collection.Client for anything not covered here.
package fkqdrant
