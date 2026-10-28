package fkqdrant

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// ErrNotFound matches (via errors.Is) every error returned when a point
	// does not exist.
	ErrNotFound = errors.New("not found")
	// ErrCollectionNotFound matches errors caused by a missing collection.
	ErrCollectionNotFound = errors.New("collection not found")
	// ErrVectorSize matches errors caused by a vector whose length or
	// distance does not match the collection's configuration.
	ErrVectorSize = errors.New("vector size mismatch")
)

// NotFoundError reports that a point of Entity does not exist.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string { return entityName(e.Entity) + " not found" }

func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

func entityName(e string) string {
	if e == "" {
		return "point"
	}
	return e
}

// mapError wraps Qdrant errors with this package's sentinel errors. The
// original error stays reachable through errors.As / status.FromError.
func mapError(entity, collection string, err error) error {
	if err == nil {
		return nil
	}
	s, _ := status.FromError(err)
	msg := s.Message()
	switch {
	case s.Code() == codes.NotFound && strings.Contains(msg, "Collection"):
		return fmt.Errorf("fkqdrant: collection %q: %w: %w", collection, ErrCollectionNotFound, err)
	case s.Code() == codes.NotFound && strings.Contains(msg, "No point"):
		return fmt.Errorf("%w: %w", &NotFoundError{Entity: entity}, err)
	case s.Code() == codes.InvalidArgument && strings.Contains(msg, "dimension"):
		return fmt.Errorf("fkqdrant: collection %q: %w: %w", collection, ErrVectorSize, err)
	}
	return fmt.Errorf("fkqdrant: collection %q: %w", collection, err)
}
