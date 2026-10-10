package data

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/sup"
)

// A Partition is a logical view of the records within a pool-key span, stored
// in one or more data objects.  This provides a way to return the list of
// objects that should be scanned along with a span to limit the scan
// to only the span involved.
type Partition struct {
	Min     super.Value `super:"min"`
	Max     super.Value `super:"max"`
	Objects []*Object   `super:"objects"`
}

func (p Partition) IsZero() bool {
	return p.Objects == nil
}

func (p Partition) FormatRangeOf(index int) string {
	o := p.Objects[index]
	return fmt.Sprintf("[%s-%s,%s-%s]", sup.String(p.Min), sup.String(p.Max), sup.String(o.Min), sup.String(o.Max))
}

func (p Partition) FormatRange() string {
	return fmt.Sprintf("[%s-%s]", sup.String(p.Min), sup.String(p.Max))
}
