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
	"github.com/superdb/super/sio/jsonio/jsonvec"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

const defaultInputCap = 10 * 1024

// defaultByteCap bounds the raw input bytes in a batch so that inputs with
// large values don't produce huge batches.  Inputs with small values hit
// defaultInputCap first.  Memory-constrained environments (e.g., wasm) may
// want a smaller cap via ReaderOpts.InputCapBytes.
const defaultByteCap = 16 * 1024 * 1024

type ReaderOpts struct {
	// InputCapBytes limits the raw input bytes in a batch.  Zero means
	// defaultByteCap.
	InputCapBytes int
}

type Reader struct {
	sctx       *super.Context
	ctx        context.Context
	stream     *stream
	byteCap    int
	projection field.Projection

	hasClosed atomic.Bool
}

// NewReader returns a Reader that parses input in batches of at most
// inputCap values or about opts.InputCapBytes bytes, whichever is reached
// first.  Zero means the default for either limit.
func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, p vio.Pushdown, concurrentReaders, inputCap int, opts ReaderOpts) *Reader {
	if inputCap == 0 {
		inputCap = defaultInputCap
	}
	byteCap := opts.InputCapBytes
	if byteCap == 0 {
		byteCap = defaultByteCap
	}
	var proj field.Projection
	if p != nil {
		// Type-only projections aren't supported so the data for
		// those paths is read.
		proj = p.Projection().NoTypes()
	}
	return &Reader{
		sctx:       sctx,
		ctx:        ctx,
		byteCap:    byteCap,
		stream:     newStream(ctx, r, concurrentReaders, inputCap, byteCap),
		projection: proj,
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
			putBytesTable(table, r.byteCap)
			r.close()
			return nil, err
		}
	}
	putBytesTable(table, r.byteCap)
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
		return jsonvec.NewProjectionBuilder(r.projection.Paths())
	}
	return jsonvec.NewBuilder()
}

func (r *Reader) close() error {
	if r.hasClosed.CompareAndSwap(false, true) {
		r.stream.close()
	}
	return nil
}
