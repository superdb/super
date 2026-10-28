package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/function"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// complete should be called invertible?

type Fuse struct {
	sctx     *super.Context
	parent   vio.Puller
	complete bool

	fuser    *super.Fuser
	vecs     []vector.Any
	upcaster *function.Upcast
	defuser  *expr.Defuse
}

func NewFuse(sctx *super.Context, parent vio.Puller, complete bool) *Fuse {
	return &Fuse{
		sctx:     sctx,
		parent:   parent,
		complete: complete,
		upcaster: function.NewUpcast(sctx),
		defuser:  expr.NewDefuse(sctx),
	}
}

func (f *Fuse) Pull(done bool) (vector.Any, error) {
	if done {
		f.fuser = nil
		f.vecs = nil
		return f.parent.Pull(done)
	}
	if f.fuser == nil {
		f.fuser = super.NewFuser(f.sctx)
		for {
			vec, err := f.parent.Pull(false)
			if err != nil {
				return nil, err
			}
			if vec == nil {
				break
			}
			f.vecs = append(f.vecs, vector.Apply(vector.ApplyNone, f.fuse, vec))
		}
	}
	if len(f.vecs) == 0 {
		f.fuser = nil
		f.vecs = nil
		return nil, nil
	}
	vec := f.vecs[0]
	f.vecs[0] = nil
	f.vecs = f.vecs[1:]
	return vector.Apply(vector.ApplyNone, f.upcast, vec), nil
}

func (f *Fuse) fuse(vecs ...vector.Any) vector.Any {
	vec := f.defuser.Eval(vecs[0])
	vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		f.fuser.Fuse(vecs[0].Type())
		return nil
	}, vec)
	return vec
}

func (f *Fuse) upcast(vecs ...vector.Any) vector.Any {
	var typ super.Type
	if f.complete {
		typ = f.fuser.FusedType()
	} else {
		typ = f.fuser.SuperType()
	}
	out, ok := f.upcaster.Cast(vecs[0], typ)
	if !ok {
		return vector.NewWrappedError(f.sctx, "cannot upcast to "+sup.FormatType(typ), vecs[0])
	}
	return out
}
