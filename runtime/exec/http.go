package exec

import (
	"errors"
	"slices"
	"strings"

	"github.com/superdb/super"
)

// The HTTP options of the from operator may be values of a named, union, or
// option type, and an option whose value is null or none is absent.

// HTTPMethod returns the HTTP method specified by val, which must be a string,
// or "" (i.e., GET) if val is absent.
func HTTPMethod(val super.Value) (string, error) {
	val, ok := present(val)
	if !ok {
		return "", nil
	}
	if !val.IsString() {
		return "", errors.New("method value must be a string")
	}
	return strings.Clone(val.AsString()), nil
}

// HTTPBody returns the HTTP request body specified by val, which must be a
// string or bytes, or nil if val is absent.
func HTTPBody(val super.Value) ([]byte, error) {
	val, ok := present(val)
	if !ok {
		return nil, nil
	}
	switch super.TypeUnder(val.Type()) {
	case super.TypeString, super.TypeBytes:
		return slices.Clone(val.Bytes()), nil
	}
	return nil, errors.New("body value must be a string or bytes")
}

// HTTPHeaders returns the HTTP headers specified by val, a record whose
// fields are each a string or (for a header appearing multiple times with
// different values) an array or set of strings, or nil if val is absent.
// Absent fields and array elements are ignored.
func HTTPHeaders(val super.Value) (map[string][]string, error) {
	val, ok := present(val)
	if !ok {
		return nil, nil
	}
	if !super.IsRecordType(val.Type()) {
		return nil, errors.New("headers value must be a record")
	}
	headers := map[string][]string{}
	for i, f := range val.Fields() {
		fieldVal := val.DerefByColumn(i)
		if fieldVal == nil {
			continue
		}
		fieldVal2, ok := present(*fieldVal)
		if !ok {
			continue
		}
		headerStrings, err := headerStrings(fieldVal2)
		if err != nil {
			return nil, err
		}
		headers[f.Name] = append(headers[f.Name], headerStrings...)
	}
	return headers, nil
}

func headerStrings(val super.Value) ([]string, error) {
	if inner := super.InnerType(val.Type()); inner != nil {
		var out []string
		for it := val.ContainerIter(); !it.Done(); {
			elem, ok := present(super.NewValue(inner, it.Next()))
			if !ok {
				continue
			}
			if !elem.IsString() {
				return nil, errors.New("array elements of header field must be strings")
			}
			out = append(out, strings.Clone(elem.AsString()))
		}
		return out, nil
	}
	if !val.IsString() {
		return nil, errors.New("header field value must be a string or an array or set of strings")
	}
	return []string{strings.Clone(val.AsString())}, nil
}

// present returns val without named, union, fusion, or option types and
// returns false if val is absent, i.e., null or none.
func present(val super.Value) (super.Value, bool) {
	val = val.Under()
	if super.IsOptionType(val.Type()) {
		if val.IsNone() {
			return val, false
		}
		val = val.Unwrap().Under()
	}
	switch val.Type().ID() {
	case super.IDNull, super.IDNone:
		return val, false
	}
	return val, true
}
