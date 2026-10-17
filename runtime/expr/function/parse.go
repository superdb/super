package function

import (
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/vector"
)

type ParseURI struct {
	sctx  *super.Context
	samfn *samfunc.ParseURI
}

func newParseURI(sctx *super.Context) *ParseURI {
	return &ParseURI{sctx, samfunc.NewParseURI(sctx)}
}

func (p *ParseURI) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(p.sctx, true, args, "parse_uri"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	if vec.Type().ID() != super.IDString {
		return vector.NewWrappedError(p.sctx, "parse_uri: string arg required", args[0])
	}
	var b scode.Builder
	db := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		b.Truncate()
		vec.Serialize(&b, i)
		val := super.NewValue(super.TypeString, b.Bytes().Body())
		db.Write(p.samfn.Call([]super.Value{val}))
	}
	return db.Build(p.sctx)
}

type ParseSUP struct {
	sctx *super.Context
	sr   *strings.Reader
	zr   *supio.Reader
}

func newParseSUP(sctx *super.Context) *ParseSUP {
	var sr strings.Reader
	return &ParseSUP{sctx, &sr, supio.NewReader(sctx, &sr)}
}

func (p *ParseSUP) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(p.sctx, true, args, "parse_sup"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	if vec.Type().ID() != super.IDString {
		return vector.NewWrappedError(p.sctx, "parse_sup: string arg required", args[0])
	}
	var errs []uint32
	errMsgs := vector.NewStringEmpty(0)
	builder := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		s := vector.StringValue(vec, i)
		p.sr.Reset(s)
		val, err := p.zr.Read()
		if err != nil {
			errs = append(errs, i)
			errMsgs.Append("parse_sup: " + err.Error())
			continue
		}
		if val == nil {
			builder.Write(super.Null)
		} else {
			builder.Write(*val)
		}
	}
	out := builder.Build(p.sctx)
	if len(errs) > 0 {
		return vector.Combine(out, errs, vector.NewVecWrappedError(p.sctx, errMsgs, vector.Pick(args[0], errs)))
	}
	return out
}
