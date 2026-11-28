package super

import (
	"errors"

	"github.com/superdb/super/scode"
)

// A Visitor is called for each value in a record encountered by
// Walk. If the visitor returns an error, the walk stops and that
// error will be returned to the caller of Walk(). The sole exception
// is when the visitor returns the special value SkipContainer.
type Visitor func(typ Type, body scode.Bytes) error

// SkipContainer is used as a return value from Visitors to indicate
// that the container passed in the call should not be visited. It is
// not returned as an error by any function.
var SkipContainer = errors.New("skip this container")

func Walk(typ Type, body scode.Bytes, visit Visitor) error {
	if err := visit(typ, body); err != nil {
		if err == SkipContainer {
			return nil
		}
		return err
	}
	switch typ := typ.(type) {
	case *TypeNamed:
		return Walk(typ.Type, body, visit)
	case *TypeRecord:
		return walkRecord(typ, body, visit)
	case *TypeArray:
		return walkArray(typ, body, visit)
	case *TypeSet:
		return walkSet(typ, body, visit)
	case *TypeUnion:
		return walkUnion(typ, body, visit)
	case *TypeMap:
		return walkMap(typ, body, visit)
	case *TypeError:
		return Walk(typ.Type, body, visit)
	case *TypeSuper:
		ftyp, body := typ.DerefSuper(body)
		return Walk(ftyp, body, visit)
	}
	return nil
}

func walkRecord(typ *TypeRecord, body scode.Bytes, visit Visitor) error {
	it := body.Iter()
	for _, f := range typ.Fields {
		if it.Done() {
			return errors.New("record missing a field")
		}
		elem := it.Next()
		if IsNone(f.Type, elem) {
			continue
		}
		if err := Walk(f.Type, elem, visit); err != nil {
			return err
		}
	}
	return nil
}

func walkArray(typ *TypeArray, body scode.Bytes, visit Visitor) error {
	inner := InnerType(typ)
	it := body.Iter()
	for !it.Done() {
		if err := Walk(inner, it.Next(), visit); err != nil {
			return err
		}
	}
	return nil
}

func walkUnion(typ *TypeUnion, body scode.Bytes, visit Visitor) error {
	if len(body) == 0 {
		return errors.New("union has empty body")
	}
	it := body.Iter()
	tag := DecodeUint(it.Next())
	inner, err := typ.Type(int(tag))
	if err != nil {
		return err
	}
	body = it.Next()
	if !it.Done() {
		return errors.New("union value container has more than two items")
	}
	return Walk(inner, body, visit)
}

func walkSet(typ *TypeSet, body scode.Bytes, visit Visitor) error {
	inner := TypeUnder(InnerType(typ))
	it := body.Iter()
	for !it.Done() {
		if err := Walk(inner, it.Next(), visit); err != nil {
			return err
		}
	}
	return nil
}

func walkMap(typ *TypeMap, body scode.Bytes, visit Visitor) error {
	keyType := TypeUnder(typ.KeyType)
	valType := TypeUnder(typ.ValType)
	it := body.Iter()
	for !it.Done() {
		if err := Walk(keyType, it.Next(), visit); err != nil {
			return err
		}
		if err := Walk(valType, it.Next(), visit); err != nil {
			return err
		}
	}
	return nil
}
