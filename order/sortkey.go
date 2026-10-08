package order

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/superdb/super/pkg/field"
)

type SortKey struct {
	Order Which      `json:"order" super:"order"`
	Path  field.Path `json:"key" super:"path"`
}

func NewSortKey(order Which, key field.Path) SortKey {
	return SortKey{order, key}
}

func (s SortKey) Equal(to SortKey) bool {
	return s.Order == to.Order && s.Path.Equal(to.Path)
}

func (s SortKey) String() string {
	return fmt.Sprintf("%s:%s", s.Path, s.Order)
}

type SortKeys []SortKey

func (s SortKeys) Primary() SortKey { return s[0] }
func (s SortKeys) IsNil() bool      { return len(s) == 0 }

func (s SortKeys) Equal(to SortKeys) bool {
	return slices.EqualFunc(s, to, func(a, b SortKey) bool {
		return a.Equal(b)
	})
}

func ParseSortKeys(s string) (SortKeys, error) {
	if s == "" {
		return nil, nil
	}
	which := Asc
	parts := strings.Split(s, ":")
	if len(parts) > 1 {
		if len(parts) > 2 {
			return nil, errors.New("only one order clause allowed in sortkey description")
		}
		var err error
		which, err = Parse(parts[1])
		if err != nil {
			return nil, err
		}
	}
	keys := field.DottedList(parts[0])
	var sortKeys []SortKey
	for _, k := range keys {
		sortKeys = append(sortKeys, NewSortKey(which, k))
	}
	return sortKeys, nil
}
