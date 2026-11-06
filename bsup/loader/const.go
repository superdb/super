package loader

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Const is a const vector, whose locators are its kind, count, and value.  A
// const vector of length zero is an empty vector.
type Const struct {
	Node
	Value []byte
}

func (c *Const) project(loader *FrameLoader, projection field.Projection) vector.Any {
	// Map the type from the frame's vector typedefs to the query context.
	typ := loader.vectorType(c.TypeID)
	if c.Length == 0 {
		return vector.NewEmpty(typ)
	}
	if typ == super.TypeNull {
		return vector.NewNull(c.Length)
	}
	return vector.NewConstFromValue(loader.sctx, super.NewValue(typ, c.Value), c.Length)
}

func (k *Const) strings(c *collector) {
	if id := k.TypeID; id < super.IDTypeComplex && id != super.IDString {
		return
	}
	// Complex consts: every embedded string appears verbatim in the
	// encoding, so the raw bytes are a safe superset to scan.
	c.append(k.Value)
}
