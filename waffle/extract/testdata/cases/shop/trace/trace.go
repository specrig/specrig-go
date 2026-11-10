package trace

import (
	"maps"
	"slices"
)

type textMapCarrier map[string]string

func (c textMapCarrier) Get(key string) string { return c[key] }
func (c textMapCarrier) Set(key, value string) { c[key] = value }
func (c textMapCarrier) Keys() []string        { return slices.Collect(maps.Keys(c)) }
