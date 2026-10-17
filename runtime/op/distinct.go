package op

import (
	"encoding/binary"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Distinct struct {
	sctx   *super.Context
	parent vio.Puller
	expr   expr.Evaluator

	blocked map[string]struct{}
	defuse  *expr.Defuse
	key     []byte
}

func NewDistinct(sctx *super.Context, parent vio.Puller, e expr.Evaluator) *Distinct {
	return &Distinct{sctx, parent, e, map[string]struct{}{}, expr.NewDefuse(sctx), nil}
}

func (d *Distinct) Pull(done bool) (vector.Any, error) {
	for {
		vec, err := d.parent.Pull(done)
		if vec == nil || err != nil {
			clear(d.blocked)
			return nil, err
		}
		var sb scode.Builder
		var index []uint32
		keyVec := d.defuse.Eval(d.expr.Eval(vec))
		for i := range keyVec.Len() {
			keyVal := vector.ValueAt(&sb, keyVec, i)
			d.key = binary.LittleEndian.AppendUint32(d.key[:0], uint32(keyVal.Type().ID()))
			d.key = append(d.key, keyVal.Bytes()...)
			if _, ok := d.blocked[string(d.key)]; !ok {
				d.blocked[string(d.key)] = struct{}{}
				index = append(index, i)
			}
		}
		if len(index) > 0 {
			return vector.Pick(vec, index), nil
		}
	}
}
