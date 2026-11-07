package exec

import (
	"errors"
	"slices"
	"strings"

	"github.com/superdb/super"
)

// HTTPMethod returns the HTTP method specified by val, which must be a string.
func HTTPMethod(val super.Value) (string, error) {
	if super.TypeUnder(val.Type()) != super.TypeString {
		return "", errors.New("method value must be a string")
	}
	return strings.Clone(val.AsString()), nil
}

// HTTPBody returns the HTTP request body specified by val, which must be a
// string or bytes.
func HTTPBody(val super.Value) ([]byte, error) {
	switch super.TypeUnder(val.Type()) {
	case super.TypeString, super.TypeBytes:
		return slices.Clone(val.Bytes()), nil
	}
	return nil, errors.New("body value must be a string or bytes")
}

// HTTPHeaders returns the HTTP headers specified by val, a record whose
// fields are each a string or (for a header appearing multiple times with
// different values) an array or set of strings.
func HTTPHeaders(val super.Value) (map[string][]string, error) {
	if !super.IsRecordType(val.Type()) {
		return nil, errors.New("headers value must be a record")
	}
	headers := map[string][]string{}
	for i, f := range val.Fields() {
		fieldVal := val.DerefByColumn(i)
		if fieldVal.IsMissing() {
			continue
		}
		headerStrings, err := headerStrings(fieldVal)
		if err != nil {
			return nil, err
		}
		headers[f.Name] = append(headers[f.Name], headerStrings...)
	}
	return headers, nil
}

func headerStrings(val *super.Value) ([]string, error) {
	typ := super.TypeUnder(val.Type())
	if inner := super.InnerType(typ); inner != nil {
		if inner.ID() != super.IDString {
			return nil, errors.New("array elements of header field must be strings")
		}
		var out []string
		for it := val.ContainerIter(); !it.Done(); {
			out = append(out, strings.Clone(super.DecodeString(it.Next())))
		}
		return out, nil
	}
	if typ != super.TypeString {
		return nil, errors.New("header field value must be a string or an array or set of strings")
	}
	return []string{strings.Clone(val.AsString())}, nil
}
