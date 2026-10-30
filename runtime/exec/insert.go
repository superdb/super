package exec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/function"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// insertShaper gives the records of an INSERT the SQL semantics of
// assigning their fields positionally to the target columns, i.e., the
// explicit column list of the INSERT or else all the table's columns.
// For a table with a record type, a shaped record has all of the table's
// columns in the table's order, where each value is cast to the column's
// type and columns without values are null.
type insertShaper struct {
	sctx    *super.Context
	named   *super.TypeNamed
	columns *super.TypeRecord
	targets []string
	casters map[string]expr.Evaluator
}

func newInsertShaper(sctx *super.Context, constraint super.Type, cols []string) (*insertShaper, error) {
	if constraint == nil && len(cols) == 0 {
		// The table has no type and there are no columns to assign
		// so the values are inserted as is.
		return nil, nil
	}
	s := &insertShaper{sctx: sctx, targets: cols}
	if constraint != nil {
		named, _ := constraint.(*super.TypeNamed)
		columns, ok := super.TypeUnder(constraint).(*super.TypeRecord)
		if !ok {
			if len(cols) > 0 {
				return nil, fmt.Errorf("INSERT column list requires a table of record type (table type is %s)", sup.FormatType(constraint))
			}
			// The table type isn't a record type so the values are
			// inserted as is.
			return nil, nil
		}
		s.named, s.columns = named, columns
		s.casters = make(map[string]expr.Evaluator)
		for _, f := range columns.Fields {
			caster, err := newCaster(sctx, f.Type)
			if err != nil {
				return nil, err
			}
			if caster != nil {
				s.casters[f.Name] = caster
			}
		}
		if len(cols) == 0 {
			for _, f := range columns.Fields {
				s.targets = append(s.targets, f.Name)
			}
		}
		for _, col := range cols {
			if !columns.HasField(col) {
				return nil, fmt.Errorf("INSERT column %q is not in table type %s", col, sup.FormatType(constraint))
			}
		}
	}
	for k, col := range s.targets {
		if slices.Contains(s.targets[:k], col) {
			return nil, fmt.Errorf("INSERT column %q specified more than once", col)
		}
	}
	return s, nil
}

// newCaster returns an evaluator casting values to the type of a column
// of type typ, ignoring any null in typ, or nil if values aren't cast
// (i.e., typ is a union of several types other than null).
func newCaster(sctx *super.Context, typ super.Type) (expr.Evaluator, error) {
	if union, ok := typ.(*super.TypeUnion); ok {
		var types []super.Type
		for _, t := range union.Types {
			if t != super.TypeNull {
				types = append(types, t)
			}
		}
		if len(types) != 1 {
			return nil, nil
		}
		typ = types[0]
	}
	lit := expr.NewLiteral(sctx, super.NewTypeValue(typ))
	if typ.ID() < super.IDTypeComplex {
		return expr.NewLiteralCast(sctx, &expr.This{}, lit)
	}
	fn, err := function.New(sctx, "cast", 2)
	if err != nil {
		return nil, err
	}
	return expr.NewCall(sctx, fn, []expr.Evaluator{&expr.This{}, lit}), nil
}

func (s *insertShaper) puller(parent vio.Puller) vio.Puller {
	return &shaperPuller{s, parent}
}

type shaperPuller struct {
	*insertShaper
	parent vio.Puller
}

func (s *shaperPuller) Pull(done bool) (vector.Any, error) {
	vec, err := s.parent.Pull(done)
	if vec == nil || err != nil {
		return vec, err
	}
	return s.shapeAny(vec)
}

// shapeAny shapes the records in vec after ripping it into vectors of a
// single type.
func (s *shaperPuller) shapeAny(vec vector.Any) (vector.Any, error) {
	var shapeErr error
	out := vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if shapeErr != nil {
			return vecs[0]
		}
		out, err := s.shape(vecs[0])
		if err != nil {
			shapeErr = err
			return vecs[0]
		}
		return out
	}, vec)
	if shapeErr != nil {
		return nil, shapeErr
	}
	return out, nil
}

func (s *shaperPuller) shape(vec vector.Any) (vector.Any, error) {
	switch vec := vector.Under(vec).(type) {
	case *vector.Record:
		return s.shapeRecord(vec)
	case *vector.View:
		// Shape only the values in the view (e.g., those that passed
		// a filter) as the others might not be valid for the columns.
		if rec, ok := vector.Under(vec.Any).(*vector.Record); ok {
			fields := make([]vector.Any, len(rec.Fields))
			for k, f := range rec.Fields {
				fields[k] = vector.Pick(f, vec.Index)
			}
			return s.shapeRecord(vector.NewRecord(rec.Typ, fields, uint32(len(vec.Index))))
		}
		picked := vector.Pick(vec.Any, vec.Index)
		if _, ok := picked.(*vector.View); ok {
			return nil, fmt.Errorf("INSERT values must be records to assign columns: encountered %s", firstValue(vec))
		}
		return s.shapeAny(picked)
	case *vector.Const:
		out, err := s.shapeAny(vec.Any)
		if err != nil {
			return nil, err
		}
		return vector.NewConst(out, vec.Len()), nil
	case *vector.Error:
		return nil, fmt.Errorf("INSERT value is an error: %s", firstValue(vec))
	}
	return nil, fmt.Errorf("INSERT values must be records to assign columns: encountered %s", firstValue(vec))
}

func (s *shaperPuller) shapeRecord(rec *vector.Record) (vector.Any, error) {
	typ := super.TypeRecordOf(rec.Type())
	if len(typ.Fields) > len(s.targets) {
		return nil, fmt.Errorf("INSERT has more expressions (%d) than target columns (%d)", len(typ.Fields), len(s.targets))
	}
	n := rec.Len()
	if s.columns == nil {
		// The table has no type so just rename the fields.
		var fields []super.Field
		for k, f := range typ.Fields {
			fields = append(fields, super.NewField(s.targets[k], f.Type))
		}
		recType, err := s.sctx.LookupTypeRecord(fields)
		if err != nil {
			return nil, err
		}
		return vector.NewRecord(recType, rec.Fields, n), nil
	}
	assigned := make(map[string]vector.Any)
	for k := range typ.Fields {
		assigned[s.targets[k]] = rec.Fields[k]
	}
	var fields []super.Field
	var vecs []vector.Any
	for _, col := range s.columns.Fields {
		vec, ok := assigned[col.Name]
		if !ok {
			if !admitsNull(col.Type) {
				return nil, fmt.Errorf("INSERT has no value for NOT NULL column %q", col.Name)
			}
			vec = vector.NewNull(n)
		} else {
			if !admitsNull(col.Type) && hasKind(vec, vector.KindNull) {
				return nil, fmt.Errorf("INSERT of null into NOT NULL column %q", col.Name)
			}
			if caster := s.casters[col.Name]; caster != nil {
				vec = caster.Eval(vec)
				if err := checkCast(vec); err != nil {
					return nil, fmt.Errorf("INSERT column %q: %w", col.Name, err)
				}
			}
		}
		fields = append(fields, super.NewField(col.Name, vec.Type()))
		vecs = append(vecs, vec)
	}
	recType, err := s.sctx.LookupTypeRecord(fields)
	if err != nil {
		return nil, err
	}
	var out vector.Any = vector.NewRecord(recType, vecs, n)
	if s.named != nil {
		out = vector.NewNamed(s.named, out)
	}
	return out, nil
}

func admitsNull(typ super.Type) bool {
	if typ == super.TypeNull {
		return true
	}
	if union, ok := typ.(*super.TypeUnion); ok {
		for _, t := range union.Types {
			if t == super.TypeNull {
				return true
			}
		}
	}
	return false
}

func hasKind(vec vector.Any, kind vector.Kind) bool {
	var found bool
	vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Kind() == kind && vecs[0].Len() > 0 {
			found = true
		}
		return vecs[0]
	}, vec)
	return found
}

// checkCast returns an error if vec contains an error value (i.e., from
// a failed cast).
func checkCast(vec vector.Any) error {
	var err error
	vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if err == nil && vecs[0].Kind() == vector.KindError && vecs[0].Len() > 0 {
			err = errors.New(firstValue(vecs[0]))
		}
		return vecs[0]
	}, vec)
	return err
}

func firstValue(vec vector.Any) string {
	return sup.FormatValue(vector.ValueAt(scode.NewBuilder(), vec, 0))
}
