package fkqdrant

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// toPayload converts v (a struct with json tags, a map, ...) to a Qdrant
// payload. v must encode to a JSON object. Integral numbers are stored as
// integers, others as doubles.
func toPayload(v any) (map[string]*qdrant.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("payload must encode to a JSON object: %w", err)
	}
	out := make(map[string]*qdrant.Value, len(m))
	for k, x := range m {
		if out[k], err = toValue(x); err != nil {
			return nil, fmt.Errorf("payload field %q: %w", k, err)
		}
	}
	return out, nil
}

func toValue(x any) (*qdrant.Value, error) {
	switch x := x.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return qdrant.NewValueInt(i), nil
		}
		f, err := x.Float64()
		if err != nil {
			return nil, err
		}
		return qdrant.NewValueDouble(f), nil
	case map[string]any:
		fields := make(map[string]*qdrant.Value, len(x))
		for k, v := range x {
			val, err := toValue(v)
			if err != nil {
				return nil, err
			}
			fields[k] = val
		}
		return qdrant.NewValueFromFields(fields), nil
	case []any:
		list := make([]*qdrant.Value, len(x))
		for i, v := range x {
			val, err := toValue(v)
			if err != nil {
				return nil, err
			}
			list[i] = val
		}
		return qdrant.NewValueFromList(list...), nil
	}
	return qdrant.NewValue(x)
}

// fromPayload decodes a Qdrant payload into T through its json tags. A
// map[string]any T receives the values directly, keeping integers as int64.
func fromPayload[T any](p map[string]*qdrant.Value) (T, error) {
	var t T
	m := qdrant.ValueMapToMap(p)
	if mp, ok := any(&t).(*map[string]any); ok {
		*mp = m
		return t, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(b, &t)
	return t, err
}
