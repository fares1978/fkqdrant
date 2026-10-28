package fkqdrant

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultBatchSize   = 256
	defaultSearchLimit = 10
	defaultScrollLimit = 100
)

// Collection provides typed operations on a Qdrant collection whose payloads
// decode into T. T is usually a struct with json tags; map[string]any also
// works. Use Client directly for anything not covered here.
//
// Point IDs are UUID strings (see NewID and IDFrom). Qdrant returns them
// lowercase and hyphenated.
type Collection[T any] struct {
	Client *qdrant.Client
	Name   string
	// Entity names the payload type in error messages, e.g. "chunk not found".
	Entity string
	// BatchSize is the number of points Upsert sends per request; 0 means 256.
	BatchSize int
}

// NewCollection returns a Collection for the collection name. entity is used
// in error messages. It does not create the collection; call EnsureCollection.
func NewCollection[T any](client *qdrant.Client, name, entity string) Collection[T] {
	return Collection[T]{Client: client, Name: name, Entity: entity}
}

// Point is a vector with its ID and payload.
type Point[T any] struct {
	ID      string    `json:"id"`
	Vector  []float32 `json:"vector,omitempty"`
	Payload T         `json:"payload"`
}

// Hit is one search result. Higher scores are closer, except for Euclid and
// Manhattan distance where Score is the distance itself.
type Hit[T any] struct {
	ID      string    `json:"id"`
	Score   float32   `json:"score"`
	Payload T         `json:"payload"`
	Vector  []float32 `json:"vector,omitempty"`
}

// SearchOpts tunes Search and SearchByID.
type SearchOpts struct {
	// Limit is the maximum number of hits; 0 means 10.
	Limit uint64
	// Offset skips that many hits, for pagination.
	Offset uint64
	// Filter restricts the search to matching points; nil searches all.
	Filter *qdrant.Filter
	// ScoreThreshold drops hits scoring worse than it; 0 means no threshold.
	ScoreThreshold float32
	// WithVectors also returns each hit's vector.
	WithVectors bool
}

// ScrollOpts tunes Scroll and All.
type ScrollOpts struct {
	// Filter restricts the result to matching points; nil returns all.
	Filter *qdrant.Filter
	// Limit is the page size; 0 means 100.
	Limit uint32
	// Offset is the ID to start from, as returned by Scroll; "" starts at
	// the beginning.
	Offset string
	// WithVectors also returns each point's vector.
	WithVectors bool
}

func (c Collection[T]) err(err error) error {
	return mapError(c.Entity, c.Name, err)
}

// EnsureCollection creates the collection with single unnamed vectors of the
// given size and distance (0 means cosine), unless it exists. An existing
// collection with a different size or distance returns ErrVectorSize.
//
// opts can adjust the create request, e.g. to set HNSW or quantization
// config; they are ignored when the collection already exists.
func (c Collection[T]) EnsureCollection(ctx context.Context, size uint64, distance qdrant.Distance, opts ...func(*qdrant.CreateCollection)) error {
	if distance == qdrant.Distance_UnknownDistance {
		distance = qdrant.Distance_Cosine
	}
	exists, err := c.Client.CollectionExists(ctx, c.Name)
	if err != nil {
		return c.err(err)
	}
	if !exists {
		req := &qdrant.CreateCollection{
			CollectionName: c.Name,
			VectorsConfig:  qdrant.NewVectorsConfig(&qdrant.VectorParams{Size: size, Distance: distance}),
		}
		for _, opt := range opts {
			opt(req)
		}
		err := c.Client.CreateCollection(ctx, req)
		if err == nil {
			return nil
		}
		// Created concurrently by another process: verify its config below.
		if status.Code(err) != codes.AlreadyExists {
			return c.err(err)
		}
	}
	info, err := c.Client.GetCollectionInfo(ctx, c.Name)
	if err != nil {
		return c.err(err)
	}
	params := info.GetConfig().GetParams().GetVectorsConfig().GetParams()
	if params == nil {
		return fmt.Errorf("fkqdrant: collection %q uses named vectors: %w", c.Name, ErrVectorSize)
	}
	if params.GetSize() != size || params.GetDistance() != distance {
		return fmt.Errorf("fkqdrant: collection %q has %d-dim %s vectors, want %d-dim %s: %w",
			c.Name, params.GetSize(), params.GetDistance(), size, distance, ErrVectorSize)
	}
	return nil
}

// EnsureIndex creates a payload index on field, which makes filtering on it
// fast. Calling it again is a no-op; a different type replaces the index.
func (c Collection[T]) EnsureIndex(ctx context.Context, field string, typ qdrant.FieldType) error {
	_, err := c.Client.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
		CollectionName: c.Name,
		Wait:           qdrant.PtrOf(true),
		FieldName:      field,
		FieldType:      typ.Enum(),
	})
	return c.err(err)
}

// EnsureTenantIndex creates a keyword index on field marked as the tenant
// key (e.g. "bot_id"), so Qdrant stores each tenant's points together and
// filtered searches on it stay fast. Filter every read on that field.
func (c Collection[T]) EnsureTenantIndex(ctx context.Context, field string) error {
	_, err := c.Client.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
		CollectionName:   c.Name,
		Wait:             qdrant.PtrOf(true),
		FieldName:        field,
		FieldType:        qdrant.FieldType_FieldTypeKeyword.Enum(),
		FieldIndexParams: qdrant.NewPayloadIndexParamsKeyword(&qdrant.KeywordIndexParams{IsTenant: qdrant.PtrOf(true)}),
	})
	return c.err(err)
}

// Exists reports whether the collection exists.
func (c Collection[T]) Exists(ctx context.Context) (bool, error) {
	ok, err := c.Client.CollectionExists(ctx, c.Name)
	return ok, c.err(err)
}

// Drop deletes the collection and all its points. Dropping a missing
// collection is a no-op.
func (c Collection[T]) Drop(ctx context.Context) error {
	_, err := c.Client.GetCollectionsClient().Delete(ctx, &qdrant.DeleteCollection{CollectionName: c.Name})
	return c.err(err)
}

// Upsert inserts points, replacing any existing points with the same IDs.
// Points are sent in batches of BatchSize and each batch is applied before
// Upsert continues; if a batch fails, earlier batches stay written.
func (c Collection[T]) Upsert(ctx context.Context, points ...Point[T]) error {
	size := c.BatchSize
	if size <= 0 {
		size = defaultBatchSize
	}
	for start := 0; start < len(points); start += size {
		batch := points[start:min(start+size, len(points))]
		structs := make([]*qdrant.PointStruct, len(batch))
		for i, p := range batch {
			payload, err := toPayload(p.Payload)
			if err != nil {
				return fmt.Errorf("fkqdrant: %s %s: %w", entityName(c.Entity), p.ID, err)
			}
			structs[i] = &qdrant.PointStruct{
				Id:      pointID(p.ID),
				Vectors: qdrant.NewVectorsDense(p.Vector),
				Payload: payload,
			}
		}
		_, err := c.Client.Upsert(ctx, &qdrant.UpsertPoints{
			CollectionName: c.Name,
			Wait:           qdrant.PtrOf(true),
			Points:         structs,
		})
		if err != nil {
			return c.err(err)
		}
	}
	return nil
}

// Get returns the points with the given IDs, including their vectors.
// Missing IDs are skipped; the order of the result is not guaranteed.
func (c Collection[T]) Get(ctx context.Context, ids ...string) ([]Point[T], error) {
	if len(ids) == 0 {
		return nil, nil
	}
	res, err := c.Client.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.Name,
		Ids:            pointIDs(ids),
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(true),
	})
	if err != nil {
		return nil, c.err(err)
	}
	return c.points(res)
}

// GetOne returns the point with the given ID, or a *NotFoundError.
func (c Collection[T]) GetOne(ctx context.Context, id string) (Point[T], error) {
	points, err := c.Get(ctx, id)
	if err != nil {
		return Point[T]{}, err
	}
	if len(points) == 0 {
		return Point[T]{}, &NotFoundError{Entity: c.Entity, ID: id}
	}
	return points[0], nil
}

// Search returns the points nearest to vector, best first.
func (c Collection[T]) Search(ctx context.Context, vector []float32, opts SearchOpts) ([]Hit[T], error) {
	return c.query(ctx, qdrant.NewQueryDense(vector), opts)
}

// SearchByID returns the points nearest to the stored point id, best first,
// without the point itself ("more like this").
func (c Collection[T]) SearchByID(ctx context.Context, id string, opts SearchOpts) ([]Hit[T], error) {
	return c.query(ctx, qdrant.NewQueryID(pointID(id)), opts)
}

func (c Collection[T]) query(ctx context.Context, q *qdrant.Query, opts SearchOpts) ([]Hit[T], error) {
	limit := opts.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	req := &qdrant.QueryPoints{
		CollectionName: c.Name,
		Query:          q,
		Filter:         opts.Filter,
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(opts.WithVectors),
	}
	if opts.Offset > 0 {
		req.Offset = &opts.Offset
	}
	if opts.ScoreThreshold != 0 {
		req.ScoreThreshold = &opts.ScoreThreshold
	}
	res, err := c.Client.Query(ctx, req)
	if err != nil {
		return nil, c.err(err)
	}
	hits := make([]Hit[T], len(res))
	for i, r := range res {
		payload, err := fromPayload[T](r.GetPayload())
		if err != nil {
			return nil, c.decodeErr(r.GetId(), err)
		}
		hits[i] = Hit[T]{
			ID:      idString(r.GetId()),
			Score:   r.GetScore(),
			Payload: payload,
			Vector:  r.GetVectors().GetVector().GetDenseVector().GetData(),
		}
	}
	return hits, nil
}

// Count returns the number of points matching filter; nil counts all.
func (c Collection[T]) Count(ctx context.Context, filter *qdrant.Filter) (uint64, error) {
	n, err := c.Client.Count(ctx, &qdrant.CountPoints{
		CollectionName: c.Name,
		Filter:         filter,
		Exact:          qdrant.PtrOf(true),
	})
	return n, c.err(err)
}

// Scroll returns one page of points ordered by ID, and the offset of the
// next page ("" when there are no more).
func (c Collection[T]) Scroll(ctx context.Context, opts ScrollOpts) ([]Point[T], string, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = defaultScrollLimit
	}
	req := &qdrant.ScrollPoints{
		CollectionName: c.Name,
		Filter:         opts.Filter,
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(opts.WithVectors),
	}
	if opts.Offset != "" {
		req.Offset = pointID(opts.Offset)
	}
	res, next, err := c.Client.ScrollAndOffset(ctx, req)
	if err != nil {
		return nil, "", c.err(err)
	}
	points, err := c.points(res)
	if err != nil {
		return nil, "", err
	}
	if next == nil {
		return points, "", nil
	}
	return points, idString(next), nil
}

// All iterates over every point matching opts.Filter, fetching opts.Limit
// points per request. Iteration stops at the first error, which is yielded.
//
//	for p, err := range chunks.All(ctx, fkqdrant.ScrollOpts{}) { ... }
func (c Collection[T]) All(ctx context.Context, opts ScrollOpts) iter.Seq2[Point[T], error] {
	return func(yield func(Point[T], error) bool) {
		for {
			points, next, err := c.Scroll(ctx, opts)
			if err != nil {
				yield(Point[T]{}, err)
				return
			}
			for _, p := range points {
				if !yield(p, nil) {
					return
				}
			}
			if next == "" {
				return
			}
			opts.Offset = next
		}
	}
}

// SetPayload merges fields into the payloads of the given points; other
// fields are kept. A missing ID returns a *NotFoundError.
func (c Collection[T]) SetPayload(ctx context.Context, fields map[string]any, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.setPayload(ctx, fields, qdrant.NewPointsSelectorIDs(pointIDs(ids)))
}

// SetPayloadWhere merges fields into the payloads of the points matching
// filter; other fields are kept.
func (c Collection[T]) SetPayloadWhere(ctx context.Context, filter *qdrant.Filter, fields map[string]any) error {
	if filter == nil {
		return errNilFilter
	}
	return c.setPayload(ctx, fields, qdrant.NewPointsSelectorFilter(filter))
}

func (c Collection[T]) setPayload(ctx context.Context, fields map[string]any, sel *qdrant.PointsSelector) error {
	payload, err := toPayload(fields)
	if err != nil {
		return fmt.Errorf("fkqdrant: %w", err)
	}
	_, err = c.Client.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.Name,
		Wait:           qdrant.PtrOf(true),
		Payload:        payload,
		PointsSelector: sel,
	})
	return c.err(err)
}

// Delete removes the points with the given IDs. Missing IDs are ignored.
func (c Collection[T]) Delete(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.delete(ctx, qdrant.NewPointsSelectorIDs(pointIDs(ids)))
}

// DeleteWhere removes the points matching filter, e.g. every chunk of a
// document. filter must not be nil; use Drop to remove everything.
func (c Collection[T]) DeleteWhere(ctx context.Context, filter *qdrant.Filter) error {
	if filter == nil {
		return errNilFilter
	}
	return c.delete(ctx, qdrant.NewPointsSelectorFilter(filter))
}

var errNilFilter = errors.New("fkqdrant: filter must not be nil")

func (c Collection[T]) delete(ctx context.Context, sel *qdrant.PointsSelector) error {
	_, err := c.Client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: c.Name,
		Wait:           qdrant.PtrOf(true),
		Points:         sel,
	})
	return c.err(err)
}

func (c Collection[T]) points(res []*qdrant.RetrievedPoint) ([]Point[T], error) {
	points := make([]Point[T], len(res))
	for i, r := range res {
		payload, err := fromPayload[T](r.GetPayload())
		if err != nil {
			return nil, c.decodeErr(r.GetId(), err)
		}
		points[i] = Point[T]{
			ID:      idString(r.GetId()),
			Vector:  r.GetVectors().GetVector().GetDenseVector().GetData(),
			Payload: payload,
		}
	}
	return points, nil
}

func (c Collection[T]) decodeErr(id *qdrant.PointId, err error) error {
	return fmt.Errorf("fkqdrant: decode %s %s: %w", entityName(c.Entity), idString(id), err)
}
