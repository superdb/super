package sup

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/scode"
	"golang.org/x/text/unicode/norm"
)

func Build(b *scode.Builder, val Value) (super.Value, error) {
	b.Truncate()
	if err := buildValue(b, val); err != nil {
		return super.Null, err
	}
	it := b.Bytes().Iter()
	return super.NewValue(val.Type(), it.Next()), nil
}

func buildValue(b *scode.Builder, val Value) error {
	switch val := val.(type) {
	case *Primitive:
		return BuildPrimitive(b, *val)
	case *Record:
		return buildRecord(b, val)
	case *Array:
		return buildArray(b, val)
	case *Set:
		return buildSet(b, val)
	case *Union:
		return buildUnion(b, val)
	case *Map:
		return buildMap(b, val)
	case *Enum:
		return buildEnum(b, val)
	case *TypeValue:
		return buildTypeValue(b, val)
	case *Error:
		return buildValue(b, val.value)
	case *Fusion:
		return buildFusion(b, val)
	case *Option:
		return buildOption(b, val)
	case *Null:
		b.Append(nil)
		return nil
	case *None:
		typ := val.Type()
		if super.TypeUnder(typ) != super.TypeMissing {
			// option nones should have been converted to Option
			// by here.
			panic(typ)

		}
		// This is a none::missing
		b.Append(nil)
		return nil
	}
	return fmt.Errorf("unknown ast type: %T", val)
}

func BuildPrimitive(b *scode.Builder, val Primitive) error {
	switch super.TypeUnder(val.typ).(type) {
	case *super.TypeOfUint8, *super.TypeOfUint16, *super.TypeOfUint32, *super.TypeOfUint64:
		v, err := strconv.ParseUint(val.text, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid unsigned integer: %s", val.text)
		}
		b.Append(super.EncodeUint(v))
		return nil
	case *super.TypeOfInt8, *super.TypeOfInt16, *super.TypeOfInt32, *super.TypeOfInt64:
		v, err := strconv.ParseInt(val.text, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid integer: %s", val.text)
		}
		b.Append(super.EncodeInt(v))
		return nil
	case *super.TypeOfDuration:
		d, err := nano.ParseDuration(val.text)
		if err != nil {
			return fmt.Errorf("invalid duration: %s", val.text)
		}
		b.Append(super.EncodeDuration(d))
		return nil
	case *super.TypeOfTime:
		t, err := time.Parse(time.RFC3339Nano, val.text)
		if err != nil {
			return fmt.Errorf("invalid ISO time: %s", val.text)
		}
		if nano.MaxTs.Time().Sub(t) < 0 {
			return fmt.Errorf("time overflow: %s (max: %s)", val.text, nano.MaxTs)
		}
		b.Append(super.EncodeTime(nano.TimeToTs(t)))
		return nil
	case *super.TypeOfFloat16:
		v, err := strconv.ParseFloat(val.text, 32)
		if err != nil {
			return fmt.Errorf("invalid floating point: %s", val.text)
		}

		b.Append(super.EncodeFloat16(float32(v)))
		return nil
	case *super.TypeOfFloat32:
		v, err := strconv.ParseFloat(val.text, 32)
		if err != nil {
			return fmt.Errorf("invalid floating point: %s", val.text)
		}
		b.Append(super.EncodeFloat32(float32(v)))
		return nil
	case *super.TypeOfFloat64:
		v, err := strconv.ParseFloat(val.text, 64)
		if err != nil {
			return fmt.Errorf("invalid floating point: %s", val.text)
		}
		b.Append(super.EncodeFloat64(v))
		return nil
	case *super.TypeOfBool:
		var v bool
		if val.text == "true" {
			v = true
		} else if val.text != "false" {
			return fmt.Errorf("invalid bool: %s", val.text)
		}
		b.Append(super.EncodeBool(v))
		return nil
	case *super.TypeOfBytes:
		s := val.text
		if len(s) < 2 || s[0:2] != "0x" {
			return fmt.Errorf("invalid bytes: %s", s)
		}
		var bytes []byte
		if len(s) == 2 {
			// '0x' is an empty byte string (not null byte string)
			bytes = []byte{}
		} else {
			var err error
			bytes, err = hex.DecodeString(s[2:])
			if err != nil {
				return fmt.Errorf("invalid bytes: %s (%w)", s, err)
			}
		}
		b.Append(scode.Bytes(bytes))
		return nil
	case *super.TypeOfString:
		body := super.EncodeString(val.text)
		if !utf8.Valid(body) {
			return fmt.Errorf("invalid utf8 string: %q", val.text)
		}
		b.Append(norm.NFC.Bytes(body))
		return nil
	case *super.TypeOfIP:
		ip, err := netip.ParseAddr(val.text)
		if err != nil {
			return err
		}
		b.Append(super.EncodeIP(ip))
		return nil
	case *super.TypeOfNet:
		net, err := netip.ParsePrefix(val.text)
		if err != nil {
			return fmt.Errorf("invalid network: %s (%w)", val.text, err)
		}
		b.Append(super.EncodeNet(net.Masked()))
		return nil
	case *super.TypeOfMissing:
		if val.text != "" {
			return fmt.Errorf("invalid text body of missing value: %q", val.text)
		}
		b.Append(nil)
		return nil
	case *super.TypeOfNull:
		if val.text != "" {
			return fmt.Errorf("invalid text body of null value: %q", val.text)
		}
		b.Append(nil)
		return nil
	case *super.TypeOfType:
		return fmt.Errorf("type values should not be encoded as primitives: %q", val.text)
	}
	return fmt.Errorf("unknown primitive: %T", val.Type)
}

func buildRecord(b *scode.Builder, val *Record) error {
	b.BeginContainer()
	for k, v := range val.fields {
		if super.TypeUnder(v.Type()) == super.TypeMissing {
			return fmt.Errorf("none::missing assigned to field %s", super.TypeUnder(val.Type()).(*super.TypeRecord).Fields[k].Name)
		}
		if err := buildValue(b, v); err != nil {
			return err
		}
	}
	b.EndContainer()
	return nil
}

func buildArray(b *scode.Builder, array *Array) error {
	b.BeginContainer()
	for _, v := range array.elems {
		if err := buildValue(b, v); err != nil {
			return err
		}
	}
	b.EndContainer()
	return nil
}

func buildSet(b *scode.Builder, set *Set) error {
	b.BeginContainer()
	for _, v := range set.elems {
		if err := buildValue(b, v); err != nil {
			return err
		}
	}
	b.TransformContainer(super.NormalizeSet)
	b.EndContainer()
	return nil
}

func buildMap(b *scode.Builder, m *Map) error {
	b.BeginContainer()
	for _, entry := range m.entries {
		if err := buildValue(b, entry.key); err != nil {
			return err
		}
		if err := buildValue(b, entry.value); err != nil {
			return err
		}
	}
	b.TransformContainer(super.NormalizeMap)
	b.EndContainer()
	return nil
}

func buildUnion(b *scode.Builder, union *Union) error {
	super.BeginUnion(b, union.tag)
	if err := buildValue(b, union.value); err != nil {
		return err
	}
	b.EndContainer()
	return nil
}

func buildFusion(b *scode.Builder, f *Fusion) error {
	b.BeginContainer()
	// supertype value
	if err := buildValue(b, f.value); err != nil {
		return err
	}
	// subtype
	b.Append(f.subtype)
	b.EndContainer()
	return nil
}

func buildOption(b *scode.Builder, o *Option) error {
	if _, ok := o.value.(*None); ok {
		super.BuildNone(b)
	} else {
		super.BeginSomeContainer(b)
		if err := buildValue(b, o.value); err != nil {
			return err
		}
		b.EndContainer()
	}
	return nil
}

func buildEnum(b *scode.Builder, enum *Enum) error {
	under, ok := super.TypeUnder(enum.typ).(*super.TypeEnum)
	if !ok {
		// This shouldn't happen.
		return errors.New("enum value is not of type enum")
	}
	selector := under.Lookup(enum.name)
	if selector < 0 {
		return fmt.Errorf("symbol %q not a member of %s", enum.name, String(enum.Type))
	}
	b.Append(super.EncodeUint(uint64(selector)))
	return nil
}

func buildTypeValue(b *scode.Builder, tv *TypeValue) error {
	b.Append(super.EncodeTypeValue(tv.value))
	return nil
}
