package jsonio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/bytedance/sonic/ast"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio/jsonio/jsonvec"
	"github.com/superdb/super/vector"
)

var DefaultInputCap uint32 = 10 * 1024

type Reader struct {
	sctx       *super.Context
	ctx        context.Context
	stream     *stream
	projection field.List

	hasClosed atomic.Bool
}

func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, p sbuf.Pushdown, concurrentReaders, inputCap int) *Reader {
	if inputCap == 0 {
		inputCap = int(DefaultInputCap)
	}
	var fields field.List
	if p != nil {
		fields = p.Projection().Paths()
	}
	return &Reader{
		sctx:       sctx,
		ctx:        ctx,
		stream:     newStream(ctx, r, concurrentReaders, inputCap),
		projection: fields,
	}
}

func (r *Reader) Pull(done bool) (vector.Any, error) {
	return r.ConcurrentPull(done, 0)
}

func (r *Reader) ConcurrentPull(done bool, _ int) (vector.Any, error) {
	if done {
		return nil, r.close()
	}
	table, start, err := r.stream.next()
	if table == nil || err != nil {
		r.close()
		return nil, err
	}
	builder := r.newBuilder()
	for i := range table.Len() {
		if err := ast.Preorder(byteconv.UnsafeString(table.Bytes(i)), builder, nil); err != nil {
			err = preorderErr(i, start, table, err)
			bytesTablePool.Put(table)
			r.close()
			return nil, err
		}
	}
	bytesTablePool.Put(table)
	return jsonvec.Materialize(r.sctx, builder), nil
}

func preorderErr(idx uint32, start int, table *vector.BytesTable, err error) error {
	b := table.RawBytes()
	off := int(table.RawOffsets()[idx])
	lineNum := start + bytes.Count(b[:off], []byte{'\n'})
	if i := firstNonWhitespaceCharacter(b[off:]); i != -1 {
		lineNum += bytes.Count(b[off:off+i], []byte{'\n'})
	}
	return fmt.Errorf("line %d: %w", lineNum, err)
}

func (r *Reader) newBuilder() jsonvec.Builder {
	if r.projection != nil {
		return jsonvec.NewProjectionBuilder(r.projection)
	}
	return jsonvec.NewBuilder()
}

func (r *Reader) close() error {
	if r.hasClosed.CompareAndSwap(false, true) {
		r.stream.close()
	}
	return nil
}
