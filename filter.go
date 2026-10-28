package fkqdrant

import (
	"reflect"

	"github.com/qdrant/go-client/qdrant"
)

// Must returns a filter matching points that satisfy all conds.
func Must(conds ...*qdrant.Condition) *qdrant.Filter {
	return &qdrant.Filter{Must: conds}
}

// Should returns a filter matching points that satisfy at least one of conds.
func Should(conds ...*qdrant.Condition) *qdrant.Filter {
	return &qdrant.Filter{Should: conds}
}

// MustNot returns a filter matching points that satisfy none of conds.
func MustNot(conds ...*qdrant.Condition) *qdrant.Filter {
	return &qdrant.Filter{MustNot: conds}
}

// Matchable are the payload value types Eq and In can match exactly,
// including named types such as `type BotID string`.
type Matchable interface {
	~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32
}

// Eq matches points whose payload field equals v. Nested fields use dots,
// e.g. "meta.lang". For ranges, full-text and geo conditions use the
// qdrant.New* helpers (qdrant.NewRange, qdrant.NewMatchText, ...).
func Eq[V Matchable](field string, v V) *qdrant.Condition {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return qdrant.NewMatchKeyword(field, rv.String())
	case reflect.Bool:
		return qdrant.NewMatchBool(field, rv.Bool())
	}
	return qdrant.NewMatchInt(field, asInt(rv))
}

// In matches points whose payload field equals any of values.
func In[V Matchable](field string, values ...V) *qdrant.Condition {
	var zero V
	switch reflect.ValueOf(zero).Kind() {
	case reflect.String:
		s := make([]string, len(values))
		for i, v := range values {
			s[i] = reflect.ValueOf(v).String()
		}
		return qdrant.NewMatchKeywords(field, s...)
	case reflect.Bool:
		conds := make([]*qdrant.Condition, len(values))
		for i, v := range values {
			conds[i] = qdrant.NewMatchBool(field, reflect.ValueOf(v).Bool())
		}
		return qdrant.NewFilterAsCondition(Should(conds...))
	}
	n := make([]int64, len(values))
	for i, v := range values {
		n[i] = asInt(reflect.ValueOf(v))
	}
	return qdrant.NewMatchInts(field, n...)
}

func asInt(rv reflect.Value) int64 {
	if rv.CanUint() {
		return int64(rv.Uint())
	}
	return rv.Int()
}
