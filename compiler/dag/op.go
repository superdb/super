package dag

// This module is derived from the GO AST design pattern in
// https://golang.org/pkg/go/ast/
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

import (
	"encoding/json"
	"reflect"
	"slices"
	"uuid"

	"github.com/superdb/super/pkg/field"
)

type Main struct {
	Types []byte     `json:"types"`
	Funcs []*FuncDef `json:"funcs"`
	Body  Seq        `json:"body"`
}

type FuncDef struct {
	Kind   string   `json:"kind" unpack:""`
	Tag    string   `json:"tag"`
	Name   string   `json:"name"`
	Params []string `json:"params"`
	Expr   Expr     `json:"expr"`
}

type Op interface {
	opNode()
}

type Seq []Op

func (seq *Seq) Prepend(front Op) {
	*seq = append([]Op{front}, *seq...)
}

func (seq *Seq) Append(op Op) {
	*seq = append(*seq, op)
}

func (seq *Seq) Delete(from, to int) {
	*seq = slices.Delete(*seq, from, to)
}

// Ops all have suffix "Op".

type (
	AggregateOp struct {
		Kind        string       `json:"kind" unpack:""`
		Limit       int          `json:"limit"`
		Keys        []Assignment `json:"keys"`
		Aggs        []Assignment `json:"aggs"`
		PartialsIn  bool         `json:"partials_in,omitempty"`
		PartialsOut bool         `json:"partials_out,omitempty"`
	}
	CombineOp struct {
		Kind string `json:"kind" unpack:""`
	}
	CountOp struct {
		Kind  string `json:"kind" unpack:""`
		Alias string `json:"alias"`
		Expr  Expr   `json:"expr"`
	}
	CutOp struct {
		Kind string       `json:"kind" unpack:""`
		Args []Assignment `json:"args"`
	}
	DebugOp struct {
		Kind   string `json:"kind" unpack:""`
		Expr   Expr   `json:"expr"`
		Filter Expr   `json:"filter"`
	}
	DistinctOp struct {
		Kind string `json:"kind" unpack:""`
		Expr Expr   `json:"expr"`
	}
	DropOp struct {
		Kind string `json:"kind" unpack:""`
		Args []Expr `json:"args"`
	}
	FilterOp struct {
		Kind string `json:"kind" unpack:""`
		Expr Expr   `json:"expr"`
	}
	ForkOp struct {
		Kind  string `json:"kind" unpack:""`
		Paths []Seq  `json:"paths"`
	}
	FuseOp struct {
		Kind     string `json:"kind" unpack:""`
		Complete bool   `json:"complete"`
	}
	HashJoinOp struct {
		Kind       string `json:"kind" unpack:""`
		Style      string `json:"style"`
		LeftAlias  string `json:"left_alias"`
		RightAlias string `json:"right_alias"`
		LeftKey    Expr   `json:"left_key"`
		RightKey   Expr   `json:"right_key"`
	}
	HeadOp struct {
		Kind  string `json:"kind" unpack:""`
		Count int    `json:"count"`
	}
	InferOp struct {
		Kind  string `json:"kind" unpack:""`
		Limit int    `json:"limit"`
	}
	JoinOp struct {
		Kind       string `json:"kind" unpack:""`
		Style      string `json:"style"`
		LeftAlias  string `json:"left_alias"`
		RightAlias string `json:"right_alias"`
		Cond       Expr   `json:"cond"`
	}
	LoadOp struct {
		Kind    string    `json:"kind" unpack:""`
		Pool    uuid.UUID `json:"pool"`
		Branch  string    `json:"branch"`
		Author  string    `json:"author"`
		Message string    `json:"message"`
		Meta    string    `json:"meta"`
	}
	MergeOp struct {
		Kind  string     `json:"kind" unpack:""`
		Exprs []SortExpr `json:"exprs"`
	}
	OutputOp struct {
		Kind string `json:"kind" unpack:""`
		Name string `json:"name"`
	}
	PassOp struct {
		Kind string `json:"kind" unpack:""`
	}
	PutOp struct {
		Kind string       `json:"kind" unpack:""`
		Args []Assignment `json:"args"`
	}
	RenameOp struct {
		Kind string       `json:"kind" unpack:""`
		Args []Assignment `json:"args"`
	}
	ScatterOp struct {
		Kind  string `json:"kind" unpack:""`
		Paths []Seq  `json:"paths"`
	}
	SkipOp struct {
		Kind  string `json:"kind" unpack:""`
		Count int    `json:"count"`
	}
	SlicerOp struct {
		Kind string `json:"kind" unpack:""`
	}
	SortOp struct {
		Kind    string     `json:"kind" unpack:""`
		Exprs   []SortExpr `json:"exprs"`
		Reverse bool       `json:"reverse"` // Always false if len(Args)>0.
	}
	SwitchOp struct {
		Kind  string `json:"kind" unpack:""`
		Expr  Expr   `json:"expr"`
		Cases []Case `json:"cases"`
	}
	TailOp struct {
		Kind  string `json:"kind" unpack:""`
		Count int    `json:"count"`
	}
	TopOp struct {
		Kind    string     `json:"kind" unpack:""`
		Limit   int        `json:"limit"`
		Exprs   []SortExpr `json:"exprs"`
		Reverse bool       `json:"reverse"` // Always false if len(Exprs)>0.
	}
	UniqOp struct {
		Kind  string `json:"kind" unpack:""`
		Cflag bool   `json:"cflag"`
	}
	UnnestOp struct {
		Kind string `json:"kind" unpack:""`
		Expr Expr   `json:"exprs"`
	}
	ValuesOp struct {
		Kind  string `json:"kind" unpack:""`
		Exprs []Expr `json:"exprs"`
	}
)

// Support types for Ops.

type (
	Assignment struct {
		Kind string `json:"kind" unpack:""`
		LHS  Expr   `json:"lhs"`
		RHS  Expr   `json:"rhs"`
	}
	Case struct {
		Expr Expr `json:"expr"`
		Path Seq  `json:"seq"`
	}
)

func (*AggregateOp) opNode() {}
func (*CombineOp) opNode()   {}
func (*CountOp) opNode()     {}
func (*CutOp) opNode()       {}
func (*DebugOp) opNode()     {}
func (*DistinctOp) opNode()  {}
func (*DropOp) opNode()      {}
func (*FilterOp) opNode()    {}
func (*ForkOp) opNode()      {}
func (*FuseOp) opNode()      {}
func (*HashJoinOp) opNode()  {}
func (*HeadOp) opNode()      {}
func (*InferOp) opNode()     {}
func (*JoinOp) opNode()      {}
func (*LoadOp) opNode()      {}
func (*MergeOp) opNode()     {}
func (*OutputOp) opNode()    {}
func (*PassOp) opNode()      {}
func (*PutOp) opNode()       {}
func (*RenameOp) opNode()    {}
func (*ScatterOp) opNode()   {}
func (*SkipOp) opNode()      {}
func (*SlicerOp) opNode()    {}
func (*SortOp) opNode()      {}
func (*SwitchOp) opNode()    {}
func (*TailOp) opNode()      {}
func (*TopOp) opNode()       {}
func (*UniqOp) opNode()      {}
func (*UnnestOp) opNode()    {}
func (*ValuesOp) opNode()    {}

// Scanner sources also implement Op and all have suffix "Scan".
type (
	CommitMetaScan struct {
		Kind      string    `json:"kind" unpack:""`
		Pool      uuid.UUID `json:"pool"`
		Commit    uuid.UUID `json:"commit"`
		Meta      string    `json:"meta"`
		Tap       bool      `json:"tap"`
		KeyPruner Expr      `json:"key_pruner"`
	}
	DBMetaScan struct {
		Kind string `json:"kind" unpack:""`
		Meta string `json:"meta"`
	}
	DeleterScan struct {
		Kind      string    `json:"kind" unpack:""`
		Pool      uuid.UUID `json:"pool"`
		Where     Expr      `json:"where"`
		KeyPruner Expr      `json:"key_pruner"`
	}
	DeleteScan struct {
		Kind   string    `json:"kind" unpack:""`
		ID     uuid.UUID `json:"id"`
		Commit uuid.UUID `json:"commit"`
	}
	FileScan struct {
		Kind     string   `json:"kind"  unpack:""`
		Paths    []string `json:"paths"`
		Format   string   `json:"format"`
		Pushdown Pushdown `json:"pushdown"`
		Type     string   `json:"type"`
	}
	ListerScan struct {
		Kind      string    `json:"kind" unpack:""`
		Pool      uuid.UUID `json:"pool"`
		Commit    uuid.UUID `json:"commit"`
		KeyPruner Expr      `json:"key_pruner"`
	}
	HTTPScan struct {
		Kind    string              `json:"kind" unpack:""`
		URL     string              `json:"url"`
		Format  string              `json:"format"`
		Method  string              `json:"method"`
		Headers map[string][]string `json:"headers"`
		Body    string              `json:"body"`
	}
	PoolScan struct {
		Kind   string    `json:"kind" unpack:""`
		ID     uuid.UUID `json:"id"`
		Commit uuid.UUID `json:"commit"`
	}
	RobotScan struct {
		Kind   string `json:"kind" unpack:""`
		Expr   Expr   `json:"expr"`
		Format string `json:"format"`
		Filter Expr   `json:"filter"`
	}
	PoolMetaScan struct {
		Kind string    `json:"kind" unpack:""`
		ID   uuid.UUID `json:"id"`
		Meta string    `json:"meta"`
	}
	NullScan struct {
		Kind string `json:"kind" unpack:""`
	}
	SeqScan struct {
		Kind      string       `json:"kind" unpack:""`
		Pool      uuid.UUID    `json:"pool"`
		Commit    uuid.UUID    `json:"commit"`
		Fields    []field.Path `json:"fields"`
		Filter    Expr         `json:"filter"`
		KeyPruner Expr         `json:"key_pruner"`
	}
)

// Support type for scanner types.
type (
	Pushdown struct {
		Projection []field.Path `json:"projection"`
		DataFilter *ScanFilter  `json:"data_filter"`
		MetaFilter *ScanFilter  `json:"meta_filter"`
		Unordered  bool         `json:"unordered"`
	}
	ScanFilter struct {
		Projection []field.Path `json:"projection"`
		Expr       Expr         `json:"expr"`
	}
)

var DBMetas = map[string]struct{}{
	"branches": {},
	"pools":    {},
}

var PoolMetas = map[string]struct{}{
	"branches": {},
}

var CommitMetas = map[string]struct{}{
	"log":        {},
	"objects":    {},
	"partitions": {},
	"rawlog":     {},
	"vectors":    {},
}

func (*CommitMetaScan) opNode() {}
func (*DBMetaScan) opNode()     {}
func (*DeleterScan) opNode()    {}
func (*DeleteScan) opNode()     {}
func (*FileScan) opNode()       {}
func (*HTTPScan) opNode()       {}
func (*ListerScan) opNode()     {}
func (*NullScan) opNode()       {}
func (*PoolMetaScan) opNode()   {}
func (*PoolScan) opNode()       {}
func (*RobotScan) opNode()      {}
func (*SeqScan) opNode()        {}

var Pass = &PassOp{Kind: "PassOp"}

// NewFilter returns a filter node for e.
func NewFilterOp(e Expr) *FilterOp {
	return &FilterOp{
		Kind: "FilterOp",
		Expr: e,
	}
}

func NewValuesOp(exprs ...Expr) *ValuesOp {
	return &ValuesOp{"ValuesOp", exprs}
}

func CopySeq(seq Seq) Seq {
	var copies Seq
	for _, o := range seq {
		copies = append(copies, CopyOp(o))
	}
	return copies
}

func CopyOp(o Op) Op {
	if o == nil {
		panic("CopyOp nil")
	}
	b, err := json.Marshal(o)
	if err != nil {
		panic(err)
	}
	copy, err := UnmarshalOp(b)
	if err != nil {
		panic(err)
	}
	return copy
}

func WalkT[T any](v reflect.Value, post func(T) T) {
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		for i := range v.Len() {
			WalkT(v.Index(i), post)
		}
	case reflect.Interface, reflect.Pointer:
		WalkT(v.Elem(), post)
	case reflect.Struct:
		for _, field := range v.Fields() {
			WalkT(field, post)
		}
	}
	if v.CanSet() {
		if t, ok := v.Interface().(T); ok {
			v.Set(reflect.ValueOf(post(t)))
		}
	}
}

func WalkTWithError[T any](v reflect.Value, post func(T) (T, error)) error {
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		for i := range v.Len() {
			if err := WalkTWithError(v.Index(i), post); err != nil {
				return err
			}
		}
	case reflect.Interface, reflect.Pointer:
		return WalkTWithError(v.Elem(), post)
	case reflect.Struct:
		for _, field := range v.Fields() {
			if err := WalkTWithError(field, post); err != nil {
				return err
			}
		}
	}
	if v.CanSet() {
		if t, ok := v.Interface().(T); ok {
			r, err := post(t)
			if err != nil {
				return err
			}
			v.Set(reflect.ValueOf(r))
		}
	}
	return nil
}
