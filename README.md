# fkqdrant

A small generic layer over the official [Qdrant Go client](https://github.com/qdrant/go-client).
You store and search your own Go structs; the package handles payload conversion,
IDs, batching and error mapping. It does not create embeddings — pass it vectors.

```sh
go get github.com/fares1978/fkqdrant@v0.1.0
go mod tidy
```

## Run Qdrant locally

```sh
docker compose up -d   # uses docker-compose.yml from this repo
```

- gRPC (used by this package): `localhost:6334`
- REST + dashboard: http://localhost:6333/dashboard

Both ports are bound to `127.0.0.1` only. In production, keep Qdrant on the internal
Docker network and let only your Go service reach it.

## Usage

```go
import (
	"github.com/fares1978/fkqdrant"
	"github.com/qdrant/go-client/qdrant"
)

type Chunk struct {
	BotID string `json:"bot_id"`
	DocID string `json:"doc_id"`
	Text  string `json:"text"`
}

// Connect (the app reads its own config).
client, err := fkqdrant.Connect(ctx, &qdrant.Config{
	Host:   os.Getenv("QDRANT_HOST"), // "" = localhost
	Port:   6334,
	APIKey: os.Getenv("QDRANT_API_KEY"),
})
if err != nil { ... }
defer client.Close()

// One collection, created once at startup. Size = your embedding model's dimension.
chunks := fkqdrant.NewCollection[Chunk](client, "chunks", "chunk")
if err := chunks.EnsureCollection(ctx, 1024, qdrant.Distance_Cosine); err != nil { ... }
if err := chunks.EnsureTenantIndex(ctx, "bot_id"); err != nil { ... }
if err := chunks.EnsureIndex(ctx, "doc_id", qdrant.FieldType_FieldTypeKeyword); err != nil { ... }

// Store.
err = chunks.Upsert(ctx, fkqdrant.Point[Chunk]{
	ID:      fkqdrant.NewID(),
	Vector:  vec, // []float32 from your embedder
	Payload: Chunk{BotID: botID, DocID: docID, Text: text},
})

// Search one tenant's chunks.
hits, err := chunks.Search(ctx, queryVec, fkqdrant.SearchOpts{
	Limit:          5,
	Filter:         fkqdrant.Must(fkqdrant.Eq("bot_id", botID)),
	ScoreThreshold: 0.5,
})
for _, h := range hits {
	fmt.Println(h.Score, h.Payload.Text)
}

// Remove a document's chunks.
err = chunks.DeleteWhere(ctx, fkqdrant.Must(fkqdrant.Eq("doc_id", docID)))
```

## Methods

| Area | Methods |
|---|---|
| Setup | `EnsureCollection`, `EnsureIndex`, `EnsureTenantIndex`, `Exists`, `Drop` |
| Write | `Upsert` (batched, `BatchSize` default 256), `SetPayload`, `SetPayloadWhere` |
| Read | `Get`, `GetOne`, `Count`, `Scroll` (one page), `All` (iterator over every point) |
| Search | `Search` (by vector), `SearchByID` ("more like this") |
| Delete | `Delete` (by IDs), `DeleteWhere` (by filter) |
| Helpers | `Connect`, `NewID`, `IDFrom`, `Must`, `Should`, `MustNot`, `Eq`, `In` |

All writes wait until Qdrant has applied them. For anything else use `c.Client`
(the go-client `*qdrant.Client`) directly.

### Payloads

`T` is converted through its `json` tags, so `omitempty`, `-`, nested structs, slices and
`time.Time` (stored as RFC 3339) all work. `map[string]any` also works as `T`.
Payloads must encode to a JSON object.

### IDs

Qdrant point IDs are UUIDs, so IDs here are UUID strings:

- `fkqdrant.NewID()` — random UUID.
- `fkqdrant.IDFrom(key)` — always the same UUID for the same key. Use it to derive a point ID
  from a MongoDB ObjectID (`IDFrom(oid.Hex())`) or from `docID + ":" + chunkIndex`, so
  re-indexing a document overwrites its points instead of duplicating them. Other languages
  can compute the same IDs: `uuid.uuid5(uuid.UUID(fkqdrant.IDNamespace), key)` in Python.

Qdrant returns UUIDs lowercase and hyphenated. Collections written by other tools with numeric
IDs are readable; those IDs appear as decimal strings.

### Filters

```go
fkqdrant.Must(fkqdrant.Eq("bot_id", botID), fkqdrant.In("lang", "ru", "en"))
fkqdrant.MustNot(fkqdrant.Eq("archived", true))
fkqdrant.Must(qdrant.NewRange("price", &qdrant.Range{Lte: qdrant.PtrOf(100.0)}))
```

`Eq` and `In` accept strings, bools, integers and named types such as `type BotID string`.
For ranges, full-text, geo and nested conditions use the go-client `qdrant.New*` helpers;
they return the same `*qdrant.Condition`. Index fields you filter on with `EnsureIndex`.

### Iterating

```go
for p, err := range chunks.All(ctx, fkqdrant.ScrollOpts{Filter: f, WithVectors: true}) {
	if err != nil { return err }
	...
}
```

## Multi-tenancy

Keep all tenants in **one collection** with a tenant field (e.g. `bot_id`), call
`EnsureTenantIndex(ctx, "bot_id")`, and filter **every** read and delete on it. This is what
Qdrant recommends over one collection per tenant: fewer resources and faster tenant-filtered
search. Collection-per-tenant also works with this package if you need hard isolation.

## Errors

```go
p, err := chunks.GetOne(ctx, id)
switch {
case errors.Is(err, fkqdrant.ErrNotFound):           // 404 — "chunk not found"
case errors.Is(err, fkqdrant.ErrCollectionNotFound): // collection missing: call EnsureCollection
case errors.Is(err, fkqdrant.ErrVectorSize):         // vector length / distance mismatch
case err != nil:                                     // 500
}
```

The original gRPC error is still wrapped, so `status.FromError(err)` works.
`Delete` and `Drop` on missing points or collections are no-ops. `SetPayload` on a missing
ID returns `ErrNotFound`.

## Tests

```sh
go test ./...                                          # unit tests only
docker compose up -d
FKQDRANT_TEST_ADDR=localhost:6334 go test ./...        # + integration tests
```

Integration tests create and drop their own randomly named collections.

## Releasing a new version

1. Commit the changes and run the tests.

2. Pick the next version ([semver](https://semver.org); while on `v0`):

   | Change | Bump | Example |
   |---|---|---|
   | Bug fix, docs, internal change | patch | `v0.1.0` → `v0.1.1` |
   | New feature or breaking API change | minor | `v0.1.1` → `v0.2.0` |

   Check the latest tag with `git describe --tags --abbrev=0`.

3. Tag the commit and push the branch and the tag:

   ```sh
   git tag -a v0.1.1 -m "v0.1.1"
   git push origin main
   git push origin v0.1.1
   ```

4. Update the projects that use it:

   ```sh
   go get github.com/fares1978/fkqdrant@v0.1.1   # or @latest
   go mod tidy
   ```

**Never move or delete a pushed tag.** The Go module proxy and checksum database
record each version permanently, so a version whose tag was moved fails to
install with a checksum mismatch. To fix a bad release, publish a new version instead.
