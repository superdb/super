package vhash

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type key interface {
	// bind attaches vec as the current input batch.
	bind(vec vector.Any)
	// hash writes the hash of each input row into hashes.
	hash(hashes []uint64)
	// combineHash folds the hash of each input row into hashes, which
	// already holds the hashes of the preceding key positions.
	combineHash(hashes []uint64)
	// equal clears eq[i] when input row rows[i] differs from stored key keyIDs[i].
	equal(rows, keyIDs []uint32, eq []bool)
	// append stores input rows as new keys.
	append(rows []uint32)
	vector(sctx *super.Context) vector.Any
}

func newKey(typ super.Type) key {
	switch typ := typ.(type) {
	case *super.TypeNamed:
		return newNamedKey(typ)
	case *super.TypeOfInt8, *super.TypeOfInt16, *super.TypeOfInt32, *super.TypeOfInt64,
		*super.TypeOfDuration, *super.TypeOfTimestamp:
		return newIntKey(typ)
	case *super.TypeOfUint8, *super.TypeOfUint16, *super.TypeOfUint32, *super.TypeOfUint64:
		return newUintKey(typ)
	case *super.TypeOfFloat16, *super.TypeOfFloat32, *super.TypeOfFloat64:
		return newFloatKey(typ)
	case *super.TypeOfUUID:
		return newUUIDKey()
	case *super.TypeOfIP:
		return newIPKey()
	case *super.TypeOfNet:
		return newNetKey()
	case *super.TypeOfString, *super.TypeOfBytes:
		return newBytesKey(typ)
	case *super.TypeOfType:
		return newTypeKey()
	default:
		// Everything else (bool, null, records, arrays, maps,
		// unions, enums, errors, options, ...) is keyed by its serialized
		// form for now.
		return newScodeKey(typ)
	}
}

func resolveVec(vec vector.Any, scratch []uint32) (vector.Any, []uint32, []uint32) {
	switch v := vector.Under(vec).(type) {
	case *vector.View:
		return v.Any, v.Index, scratch
	case *vector.Dict:
		scratch = resize(scratch, len(v.Index))
		for i, slot := range v.Index {
			scratch[i] = uint32(slot)
		}
		return v.Any, scratch, scratch
	case *vector.Const:
		scratch = resize(scratch, int(v.Len()))
		clear(scratch)
		return v.Any, scratch, scratch
	default:
		return v, nil, scratch
	}
}
