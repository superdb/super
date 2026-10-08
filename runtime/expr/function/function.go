package function

import (
	"errors"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/anymath"
	"github.com/superdb/super/runtime/expr"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

var (
	ErrBadArgument    = errors.New("bad argument")
	ErrNoSuchFunction = errors.New("no such function")
	ErrTooFewArgs     = errors.New("too few arguments")
	ErrTooManyArgs    = errors.New("too many arguments")
)

func New(sctx *super.Context, name string, narg int) (expr.Function, error) {
	argmin := 1
	argmax := 1
	var f expr.Function
	switch name {
	case "abs":
		f = &Abs{sctx}
	case "base64":
		f = &Base64{sctx}
	case "bucket":
		argmin = 2
		argmax = 2
		f = &Bucket{sctx: sctx, name: name}
	case "cast":
		argmin = 2
		argmax = 2
		f = newCaster(sctx)
	case "ceil":
		f = &Ceil{sctx}
	case "cidr_match":
		argmin = 2
		argmax = 2
		f = NewCIDRMatch(sctx)
	case "coalesce":
		argmax = -1
		f = &Coalesce{}
	case "compare":
		argmin = 2
		argmax = 3
		f = newSamFunc(sctx, function.NewCompare(sctx))
	case "concat":
		argmin = 1
		argmax = -1
		f = &Concat{sctx: sctx}
	case "date_part":
		argmin = 2
		argmax = 2
		f = &DatePart{sctx}
	case "defuse":
		f = defuse{}
	case "downcast":
		argmin = 2
		argmax = 2
		f = newDowncast(sctx)
	case "error":
		f = &Error{sctx}
	case "fields":
		f = NewFields(sctx)
	case "flatten":
		f = newFlatten(sctx)
	case "floor":
		f = &Floor{sctx}
	case "greatest":
		argmax = -1
		f = newSamFunc(sctx, function.NewReducer(sctx, name, anymath.Max))
	case "grep":
		argmin = 2
		argmax = 2
		f = &Grep{sctx: sctx}
	case "grok":
		argmin, argmax = 2, 3
		f = newGrok(sctx)
	case "has_error":
		f = HasError{sctx}
	case "hex":
		f = &Hex{sctx}
	case "is":
		argmin = 2
		argmax = 2
		f = &Is{sctx}
	case "is_error":
		f = IsErr{}
	case "join":
		argmax = 2
		f = &Join{sctx: sctx}
	case "kind":
		f = &Kind{sctx: sctx}
	case "uuid":
		argmin = 0
		argmax = 1
		f = &UUID{sctx}
	case "least":
		argmax = -1
		f = newSamFunc(sctx, function.NewReducer(sctx, name, anymath.Min))
	case "len", "length":
		f = newLen(sctx)
	case "levenshtein":
		argmin, argmax = 2, 2
		f = &Levenshtein{sctx}
	case "log":
		f = &Log{sctx}
	case "lower":
		f = &ToLower{sctx}
	case "nameof":
		f = &NameOf{sctx: sctx}
	case "nest_dotted":
		f = &NestDotted{sctx}
	case "now":
		argmax = 0
		argmin = 0
		f = &Now{}
	case "network_of":
		argmax = 2
		f = &NetworkOf{sctx}
	case "nullif":
		argmin, argmax = 2, 2
		f = newNullIf(sctx)
	case "ok":
		f = newOk(sctx)
	case "parse_sup":
		f = newParseSUP(sctx)
	case "parse_uri":
		f = newParseURI(sctx)
	case "position":
		argmin, argmax = 2, 2
		f = &Position{sctx}
	case "pow":
		argmin = 2
		argmax = 2
		f = &Pow{sctx}
	case "regexp":
		argmin, argmax = 2, 2
		f = &Regexp{sctx: sctx}
	case "regexp_replace":
		argmin, argmax = 3, 3
		f = &RegexpReplace{sctx: sctx}
	case "replace":
		argmin, argmax = 3, 3
		f = &Replace{sctx}
	case "round":
		f = &Round{sctx}
	case "some":
		f = newSome(sctx)
	case "split":
		argmin, argmax = 2, 2
		f = &Split{sctx}
	case "sqrt":
		f = &Sqrt{sctx}
	case "strftime":
		argmin, argmax = 2, 2
		f = &Strftime{sctx: sctx}
	case "trim":
		f = &Trim{sctx}
	case "typename":
		f = &TypeName{sctx: sctx}
	case "typeof":
		f = &TypeOf{sctx}
	case "unblend":
		f = &Unblend{sctx}
	case "under":
		f = &Under{}
	case "unwrap":
		f = newUnwrap(sctx)
	case "unflatten":
		f = newUnflatten(sctx)
	case "upcast":
		argmin, argmax = 2, 2
		f = NewUpcast(sctx)
	case "upper":
		f = &ToUpper{sctx}
	default:
		return nil, function.ErrNoSuchFunction
	}
	if err := CheckArgCount(narg, argmin, argmax); err != nil {
		return nil, err
	}
	return f, nil
}

func CheckArgCount(narg int, argmin int, argmax int) error {
	if argmin != -1 && narg < argmin {
		return ErrTooFewArgs
	}
	if argmax != -1 && narg > argmax {
		return ErrTooManyArgs
	}
	return nil
}

type NeedsInput interface {
	needsInput()
}

func underAll(args []vector.Any) []vector.Any {
	out := slices.Clone(args)
	for i := range args {
		out[i] = vector.Under(args[i])
	}
	return out
}

type samFunc struct {
	sctx *super.Context
	fn   samexpr.Function

	builders []scode.Builder
	values   []super.Value
}

func newSamFunc(sctx *super.Context, fn samexpr.Function) *samFunc {
	return &samFunc{sctx: sctx, fn: fn}
}

func (f *samFunc) Call(args ...vector.Any) vector.Any {
	f.builders = slices.Grow(f.builders[:0], len(args))[:len(args)]
	b := vector.NewDynamicValueBuilder()
	for i := range args[0].Len() {
		f.values = f.values[:0]
		for j, vec := range args {
			val := vector.ValueAt(&f.builders[j], vec, i)
			f.values = append(f.values, val)
		}
		b.Write(f.fn.Call(f.values))
	}
	return b.Build(f.sctx)
}

// HasBoolResult returns true if the function name returns a Boolean value.
// XXX This is a hack so the semantic compiler can determine if a single call
// expr is a Filter or Put proc. At some point function declarations should have
// signatures so the return type can be introspected.
func HasBoolResult(name string) bool {
	switch name {
	case "grep", "has_error", "is", "is_error", "is_ok", "missing", "cidr_match":
		return true
	}
	return false
}
