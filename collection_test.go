package fkqdrant

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

type chunk struct {
	BotID string `json:"bot_id"`
	DocID string `json:"doc_id"`
	Text  string `json:"text"`
	Index int    `json:"index"`
}

// testCollection returns an empty 3-dim cosine collection that is dropped
// after the test. It skips unless FKQDRANT_TEST_ADDR (e.g. localhost:6334)
// is set.
func testCollection(t *testing.T) Collection[chunk] {
	t.Helper()
	addr := os.Getenv("FKQDRANT_TEST_ADDR")
	if addr == "" {
		t.Skip("FKQDRANT_TEST_ADDR not set")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	ctx := context.Background()
	client, err := Connect(ctx, &qdrant.Config{Host: host, Port: port, SkipCompatibilityCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	c := NewCollection[chunk](client, "fkqdrant_test_"+NewID()[:8], "chunk")
	c.BatchSize = 3
	if err := c.EnsureCollection(ctx, 3, qdrant.Distance_Cosine); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Drop(context.Background())
		client.Close()
	})
	return c
}

// seed stores 10 chunks: 6 of bot "a" (doc "d1") and 4 of bot "b" (doc "d2").
// Point i has ID IDFrom(i) and a vector close to [1,0,0] for low i.
func seed(t *testing.T, c Collection[chunk]) {
	t.Helper()
	var points []Point[chunk]
	for i := range 10 {
		ch := chunk{BotID: "a", DocID: "d1", Text: "chunk " + strconv.Itoa(i), Index: i}
		if i >= 6 {
			ch.BotID, ch.DocID = "b", "d2"
		}
		points = append(points, Point[chunk]{
			ID:      IDFrom(strconv.Itoa(i)),
			Vector:  []float32{1, float32(i) / 10, 0},
			Payload: ch,
		})
	}
	if err := c.Upsert(context.Background(), points...); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCollection(t *testing.T) {
	c := testCollection(t)
	ctx := context.Background()
	if err := c.EnsureCollection(ctx, 3, qdrant.Distance_Cosine); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if err := c.EnsureCollection(ctx, 4, qdrant.Distance_Cosine); !errors.Is(err, ErrVectorSize) {
		t.Fatalf("size mismatch: got %v", err)
	}
	if err := c.EnsureCollection(ctx, 3, qdrant.Distance_Dot); !errors.Is(err, ErrVectorSize) {
		t.Fatalf("distance mismatch: got %v", err)
	}
	if err := c.EnsureTenantIndex(ctx, "bot_id"); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureIndex(ctx, "index", qdrant.FieldType_FieldTypeInteger); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureTenantIndex(ctx, "bot_id"); err != nil {
		t.Fatalf("index again: %v", err)
	}
}

func TestUpsertGet(t *testing.T) {
	c := testCollection(t)
	ctx := context.Background()
	seed(t, c)

	n, err := c.Count(ctx, nil)
	if err != nil || n != 10 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	p, err := c.GetOne(ctx, IDFrom("2"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Payload.Text != "chunk 2" || p.Payload.Index != 2 || len(p.Vector) != 3 {
		t.Fatalf("GetOne = %+v", p)
	}
	points, err := c.Get(ctx, IDFrom("1"), IDFrom("3"), NewID())
	if err != nil || len(points) != 2 {
		t.Fatalf("Get = %d points, %v", len(points), err)
	}
	_, err = c.GetOne(ctx, NewID())
	var nf *NotFoundError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &nf) || err.Error() != "chunk not found" {
		t.Fatalf("GetOne missing: %v", err)
	}

	// Upsert replaces.
	p.Payload.Text = "replaced"
	if err := c.Upsert(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.GetOne(ctx, p.ID); p.Payload.Text != "replaced" {
		t.Fatalf("after replace: %+v", p)
	}
}

func TestSearch(t *testing.T) {
	c := testCollection(t)
	ctx := context.Background()
	seed(t, c)

	hits, err := c.Search(ctx, []float32{1, 0, 0}, SearchOpts{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].ID != IDFrom("0") || hits[0].Payload.Index != 0 || hits[0].Score < hits[1].Score {
		t.Fatalf("Search = %+v", hits)
	}
	if hits[0].Vector != nil {
		t.Fatal("vector returned without WithVectors")
	}

	hits, err = c.Search(ctx, []float32{1, 0, 0}, SearchOpts{Filter: Must(Eq("bot_id", "b")), WithVectors: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 || hits[0].Payload.Index != 6 || len(hits[0].Vector) != 3 {
		t.Fatalf("filtered Search = %+v", hits)
	}

	hits, err = c.Search(ctx, []float32{0, 0, 1}, SearchOpts{ScoreThreshold: 0.5})
	if err != nil || len(hits) != 0 {
		t.Fatalf("threshold Search = %+v, %v", hits, err)
	}

	hits, err = c.SearchByID(ctx, IDFrom("0"), SearchOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID == IDFrom("0") {
		t.Fatalf("SearchByID = %+v", hits)
	}

	page, err := c.Search(ctx, []float32{1, 0, 0}, SearchOpts{Limit: 2, Offset: 2})
	if err != nil || len(page) != 2 || page[0].Payload.Index != 2 {
		t.Fatalf("Search offset = %+v, %v", page, err)
	}
}

func TestScroll(t *testing.T) {
	c := testCollection(t)
	ctx := context.Background()
	seed(t, c)

	seen := map[string]bool{}
	opts := ScrollOpts{Limit: 4}
	for pages := 0; ; pages++ {
		points, next, err := c.Scroll(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range points {
			seen[p.ID] = true
		}
		if next == "" {
			if pages != 2 {
				t.Fatalf("got %d pages, want 3", pages+1)
			}
			break
		}
		opts.Offset = next
	}
	if len(seen) != 10 {
		t.Fatalf("Scroll saw %d points", len(seen))
	}

	count := 0
	for p, err := range c.All(ctx, ScrollOpts{Filter: Must(Eq("bot_id", "a")), Limit: 4}) {
		if err != nil {
			t.Fatal(err)
		}
		if p.Payload.BotID != "a" {
			t.Fatalf("All yielded %+v", p)
		}
		count++
	}
	if count != 6 {
		t.Fatalf("All yielded %d points, want 6", count)
	}
}

func TestSetPayloadDelete(t *testing.T) {
	c := testCollection(t)
	ctx := context.Background()
	seed(t, c)

	if err := c.SetPayload(ctx, map[string]any{"text": "edited"}, IDFrom("1")); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.GetOne(ctx, IDFrom("1")); p.Payload.Text != "edited" || p.Payload.BotID != "a" {
		t.Fatalf("after SetPayload: %+v", p.Payload)
	}
	if err := c.SetPayload(ctx, map[string]any{"text": "x"}, NewID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetPayload missing: %v", err)
	}
	if err := c.SetPayloadWhere(ctx, Must(Eq("doc_id", "d2")), map[string]any{"doc_id": "d3"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Count(ctx, Must(Eq("doc_id", "d3"))); n != 4 {
		t.Fatalf("after SetPayloadWhere: %d points of d3", n)
	}

	if err := c.Delete(ctx, IDFrom("0"), NewID()); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteWhere(ctx, Must(Eq("doc_id", "d3"))); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Count(ctx, nil); n != 5 {
		t.Fatalf("after deletes: %d points, want 5", n)
	}
	if err := c.DeleteWhere(ctx, nil); err == nil {
		t.Fatal("DeleteWhere(nil) should fail")
	}
}

func TestErrors(t *testing.T) {
	c := testCollection(t)
	ctx := context.Background()

	err := c.Upsert(ctx, Point[chunk]{ID: NewID(), Vector: []float32{1, 2}})
	if !errors.Is(err, ErrVectorSize) {
		t.Fatalf("wrong dim upsert: %v", err)
	}
	if _, err := c.Search(ctx, []float32{1}, SearchOpts{}); !errors.Is(err, ErrVectorSize) {
		t.Fatalf("wrong dim search: %v", err)
	}

	missing := NewCollection[chunk](c.Client, "fkqdrant_missing_"+NewID()[:8], "chunk")
	if _, err := missing.Search(ctx, []float32{1, 0, 0}, SearchOpts{}); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("missing collection: %v", err)
	}
	if ok, err := missing.Exists(ctx); ok || err != nil {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	if err := missing.Drop(ctx); err != nil {
		t.Fatalf("Drop missing: %v", err)
	}
}
