package dag

import (
	"fmt"

	"github.com/superdb/super/pkg/unpack"
)

var unpacker = unpack.New(
	AggExpr{},
	AggregateOp{},
	ArrayExpr{},
	Assignment{},
	BadExpr{},
	BinaryExpr{},
	CallExpr{},
	CombineOp{},
	CommitMetaScan{},
	CondExpr{},
	CountOp{},
	CutOp{},
	DebugOp{},
	DeleterScan{},
	DeleteScan{},
	DistinctOp{},
	DotExpr{},
	DropOp{},
	Field{},
	FileScan{},
	FilterOp{},
	FuncDef{},
	ForkOp{},
	FuseOp{},
	HashJoinOp{},
	HeadOp{},
	HTTPScan{},
	IndexExpr{},
	InferOp{},
	IsNullExpr{},
	JoinOp{},
	DBMetaScan{},
	ListerScan{},
	LoadOp{},
	MapCallExpr{},
	MapExpr{},
	MergeOp{},
	NullScan{},
	OutputOp{},
	PassOp{},
	PoolMetaScan{},
	PoolScan{},
	PrimitiveExpr{},
	PutOp{},
	RecordExpr{},
	RegexpMatchExpr{},
	RegexpSearchExpr{},
	RenameOp{},
	RobotScan{},
	ScatterOp{},
	SearchExpr{},
	SeqScan{},
	SetExpr{},
	SkipOp{},
	SliceExpr{},
	SlicerOp{},
	SortOp{},
	Spread{},
	SubqueryExpr{},
	SwitchOp{},
	TailOp{},
	ThisExpr{},
	TopOp{},
	TypeExpr{},
	UnaryExpr{},
	UniqOp{},
	UnnestOp{},
	ValuesOp{},
	VectorValue{},
)

// UnmarshalOp transforms a JSON representation of an operator into an Op.
func UnmarshalOp(buf []byte) (Op, error) {
	var op Op
	if err := unpacker.Unmarshal(buf, &op); err != nil {
		return nil, fmt.Errorf("internal error: JSON object is not a DAG operator: %w", err)
	}
	return op, nil
}
