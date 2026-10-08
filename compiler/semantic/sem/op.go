// Package sem represents a semantic-internal AST representation that
// is used for analysis between the AST and DAG representations.
// This is never serialized so there are no Kind fields or unpacker hooks.
// The sem AST has no scope and all functions, ops, etc. are resolved
// to flattened tables.  Likewise, all SQL  structure is translated to
// native dataflow here with backpointers to the original AST nodes for
// error reporting.
package sem

import (
	"maps"
	"slices"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/order"
)

// Op is the interface implemented by all AST operator nodes.
type Op interface {
	opNode()
	ast.Node
}

// Scanner ops source data and implement Op.
type (
	CommitMetaScan struct {
		ast.Node
		Pool   uuid.UUID
		Commit uuid.UUID
		Meta   string
		Tap    bool
	}
	DBMetaScan struct {
		ast.Node
		Meta string
	}
	DeleteScan struct {
		ast.Node
		ID     uuid.UUID
		Commit uuid.UUID
	}
	FileScan struct {
		ast.Node
		Type   super.Type
		Paths  []string
		Format string
	}
	HTTPScan struct {
		ast.Node
		URL     string
		Format  string
		Method  string
		Headers map[string][]string
		Body    string
	}
	NullScan struct {
		ast.Node
	}
	PoolMetaScan struct {
		ast.Node
		ID   uuid.UUID
		Meta string
	}
	PoolScan struct {
		ast.Node
		ID     uuid.UUID
		Commit uuid.UUID
	}
	RobotScan struct {
		ast.Node
		Expr   Expr
		Format string
	}
)

func (*CommitMetaScan) opNode() {}
func (*DBMetaScan) opNode()     {}
func (*DeleteScan) opNode()     {}
func (*FileScan) opNode()       {}
func (*HTTPScan) opNode()       {}
func (*NullScan) opNode()       {}
func (*PoolMetaScan) opNode()   {}
func (*PoolScan) opNode()       {}
func (*RobotScan) opNode()      {}

type Seq []Op

func (s *Seq) Prepend(front Op) {
	*s = append([]Op{front}, *s...)
}

func (seq *Seq) Append(op Op) {
	*seq = append(*seq, op)
}

type (
	AggregateOp struct {
		ast.Node
		Limit int
		Keys  []Assignment
		Aggs  []Assignment
	}
	BadOp struct {
		ast.Node
	}
	CountOp struct {
		ast.Node
		Alias string
		Expr  Expr
	}
	CutOp struct {
		ast.Node
		Args []Assignment
	}
	DebugOp struct {
		ast.Node
		Expr   Expr
		Filter Expr
	}
	DistinctOp struct {
		ast.Node
		Expr Expr
	}
	DropOp struct {
		ast.Node
		Args []Expr
	}
	FilterOp struct {
		ast.Node
		Expr Expr
	}
	ForkOp struct {
		ast.Node
		Paths []Seq
	}
	FuseOp struct {
		ast.Node
		Complete bool
	}
	HeadOp struct {
		ast.Node
		Count int
	}
	InferOp struct {
		ast.Node
		Limit int
	}
	JoinOp struct {
		ast.Node
		Style      string
		LeftAlias  string
		RightAlias string
		Cond       Expr
	}
	LoadOp struct {
		ast.Node
		Pool    uuid.UUID
		Branch  string
		Author  string
		Message string
		Meta    string
	}
	MergeOp struct {
		ast.Node
		Exprs []SortExpr
	}
	OutputOp struct {
		ast.Node
		Name string
	}
	PassOp struct {
		ast.Node
	}
	PutOp struct {
		ast.Node
		Args []Assignment
	}
	RenameOp struct {
		ast.Node
		Args []Assignment
	}
	SkipOp struct {
		ast.Node
		Count int
	}
	SortOp struct {
		ast.Node
		Exprs   []SortExpr
		Reverse bool
	}
	SwitchOp struct {
		ast.Node
		Expr  Expr
		Cases []Case
	}
	TailOp struct {
		ast.Node
		Count int
	}
	TopOp struct {
		ast.Node
		Limit   int
		Exprs   []SortExpr
		Reverse bool
	}
	UniqOp struct {
		ast.Node
		Cflag bool
	}
	UnnestOp struct {
		ast.Node
		Expr Expr
		Body Seq
	}
	ValuesOp struct {
		ast.Node
		Exprs []Expr
	}
)

// Suport structs for Ops
type (
	Assignment struct {
		ast.Node
		LHS Expr
		RHS Expr
	}
	// Case paths inside of SwitchOp
	Case struct {
		Expr Expr
		Path Seq
	}
	SortExpr struct {
		ast.Node
		Expr  Expr
		Order order.Which
		Nulls order.Nulls
	}
)

func (*AggregateOp) opNode() {}
func (*BadOp) opNode()       {}
func (*CountOp) opNode()     {}
func (*CutOp) opNode()       {}
func (*DebugOp) opNode()     {}
func (*DistinctOp) opNode()  {}
func (*DropOp) opNode()      {}
func (*FilterOp) opNode()    {}
func (*ForkOp) opNode()      {}
func (*FuseOp) opNode()      {}
func (*HeadOp) opNode()      {}
func (*InferOp) opNode()     {}
func (*JoinOp) opNode()      {}
func (*LoadOp) opNode()      {}
func (*MergeOp) opNode()     {}
func (*OutputOp) opNode()    {}
func (*PassOp) opNode()      {}
func (*PutOp) opNode()       {}
func (*RenameOp) opNode()    {}
func (*SkipOp) opNode()      {}
func (*SortOp) opNode()      {}
func (*SwitchOp) opNode()    {}
func (*TailOp) opNode()      {}
func (*TopOp) opNode()       {}
func (*UnnestOp) opNode()    {}
func (*UniqOp) opNode()      {}
func (*ValuesOp) opNode()    {}

func NewValues(n ast.Node, expr ...Expr) *ValuesOp {
	return &ValuesOp{Node: n, Exprs: expr}
}

func NewFilter(n ast.Node, expr Expr) *FilterOp {
	return &FilterOp{Node: n, Expr: expr}
}

func CopySeq(seq Seq) Seq {
	var out Seq
	for _, op := range seq {
		out = append(out, CopyOp(op))
	}
	return out
}

func CopyOp(op Op) Op {
	switch op := op.(type) {
	case *CommitMetaScan:
		return &CommitMetaScan{
			Node:   op.Node,
			Pool:   op.Pool,
			Commit: op.Commit,
			Meta:   op.Meta,
			Tap:    op.Tap,
		}
	case *DBMetaScan:
		return &DBMetaScan{
			Node: op.Node,
			Meta: op.Meta,
		}
	case *DeleteScan:
		return &DeleteScan{
			Node:   op.Node,
			ID:     op.ID,
			Commit: op.Commit,
		}
	case *FileScan:
		return &FileScan{
			Node:   op.Node,
			Type:   op.Type,
			Paths:  slices.Clone(op.Paths),
			Format: op.Format,
		}
	case *HTTPScan:
		return &HTTPScan{
			Node:    op.Node,
			URL:     op.URL,
			Format:  op.Format,
			Method:  op.Method,
			Headers: maps.Clone(op.Headers),
			Body:    op.Body,
		}
	case *NullScan:
		return &NullScan{
			Node: op.Node,
		}
	case *PoolMetaScan:
		return &PoolMetaScan{
			Node: op.Node,
			ID:   op.ID,
			Meta: op.Meta,
		}
	case *PoolScan:
		return &PoolScan{
			Node:   op.Node,
			ID:     op.ID,
			Commit: op.Commit,
		}
	case *RobotScan:
		return &RobotScan{
			Node:   op.Node,
			Expr:   CopyExpr(op.Expr),
			Format: op.Format,
		}

	case *AggregateOp:
		return &AggregateOp{
			Node:  op.Node,
			Limit: op.Limit,
			Keys:  copyAssignments(op.Keys),
			Aggs:  copyAssignments(op.Aggs),
		}
	case *BadOp:
		return &BadOp{Node: op.Node}
	case *CountOp:
		return &CountOp{
			Node:  op.Node,
			Alias: op.Alias,
			Expr:  CopyExpr(op.Expr),
		}
	case *CutOp:
		return &CutOp{
			Node: op.Node,
			Args: copyAssignments(op.Args),
		}
	case *DebugOp:
		return &DebugOp{
			Node: op.Node,
			Expr: CopyExpr(op.Expr),
		}
	case *DistinctOp:
		return &DistinctOp{
			Node: op.Node,
			Expr: CopyExpr(op.Expr),
		}
	case *DropOp:
		return &DropOp{
			Node: op.Node,
			Args: copyExprs(op.Args),
		}
	case *FilterOp:
		return &FilterOp{
			Node: op.Node,
			Expr: CopyExpr(op.Expr),
		}
	case *ForkOp:
		var paths []Seq
		for _, seq := range op.Paths {
			paths = append(paths, CopySeq(seq))
		}
		return &ForkOp{
			Node:  op.Node,
			Paths: paths,
		}
	case *FuseOp:
		return &FuseOp{
			Node:     op.Node,
			Complete: op.Complete,
		}
	case *HeadOp:
		return &HeadOp{
			Node:  op.Node,
			Count: op.Count,
		}
	case *JoinOp:
		return &JoinOp{
			Node:       op.Node,
			Style:      op.Style,
			LeftAlias:  op.LeftAlias,
			RightAlias: op.RightAlias,
			Cond:       CopyExpr(op.Cond),
		}
	case *LoadOp:
		return &LoadOp{
			Node:    op.Node,
			Pool:    op.Pool,
			Branch:  op.Branch,
			Author:  op.Author,
			Message: op.Message,
			Meta:    op.Meta,
		}
	case *MergeOp:
		return &MergeOp{
			Node:  op.Node,
			Exprs: copySortExprs(op.Exprs),
		}
	case *OutputOp:
		return &OutputOp{
			Node: op.Node,
			Name: op.Name,
		}
	case *PassOp:
		return &PassOp{Node: op.Node}
	case *PutOp:
		return &PutOp{
			Node: op.Node,
			Args: copyAssignments(op.Args),
		}
	case *RenameOp:
		return &RenameOp{
			Node: op.Node,
			Args: copyAssignments(op.Args),
		}
	case *SkipOp:
		return &SkipOp{
			Node:  op.Node,
			Count: op.Count,
		}
	case *SortOp:
		return &SortOp{
			Node:    op.Node,
			Exprs:   copySortExprs(op.Exprs),
			Reverse: op.Reverse,
		}
	case *SwitchOp:
		var cases []Case
		for _, c := range op.Cases {
			cases = append(cases, Case{
				Expr: CopyExpr(c.Expr),
				Path: CopySeq(c.Path),
			})
		}
		return &SwitchOp{
			Node:  op.Node,
			Expr:  CopyExpr(op.Expr),
			Cases: cases,
		}
	case *TailOp:
		return &TailOp{
			Node:  op.Node,
			Count: op.Count,
		}
	case *TopOp:
		return &TopOp{
			Node:    op.Node,
			Limit:   op.Limit,
			Exprs:   copySortExprs(op.Exprs),
			Reverse: op.Reverse,
		}
	case *UniqOp:
		return &UniqOp{
			Node:  op.Node,
			Cflag: op.Cflag,
		}
	case *UnnestOp:
		return &UnnestOp{
			Node: op.Node,
			Expr: CopyExpr(op.Expr),
			Body: CopySeq(op.Body),
		}
	case *ValuesOp:
		return &ValuesOp{
			Node:  op.Node,
			Exprs: copyExprs(op.Exprs),
		}
	default:
		panic(op)
	}
}

func copyAssignments(assignment []Assignment) []Assignment {
	var out []Assignment
	for _, a := range assignment {
		out = append(out, Assignment{
			Node: a.Node,
			LHS:  CopyExpr(a.LHS),
			RHS:  CopyExpr(a.RHS),
		})
	}
	return out
}

func copySortExprs(exprs []SortExpr) []SortExpr {
	var out []SortExpr
	for _, e := range exprs {
		out = append(out, SortExpr{
			Node:  e.Node,
			Expr:  CopyExpr(e.Expr),
			Order: e.Order,
			Nulls: e.Nulls,
		})
	}
	return out
}
