package op

import (
	"bytes"
	"io"
	"net/url"
	"uuid"

	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Robot struct {
	parent   vio.Puller
	rctx     *runtime.Context
	env      *exec.Environment
	expr     expr.Evaluator
	pushdown vio.Pushdown
	progress *vio.Progress
	format   string
	// The HTTP options are each nil if absent.  If they're all absent, the
	// robot reads its inputs like any other input.
	method  expr.Evaluator
	headers expr.Evaluator
	body    expr.Evaluator
	vec     vector.Any
	// optVecs holds the values of the HTTP options for each value in vec.
	optVecs httpOptVecs
	off     uint32
	src     vio.Puller
}

type httpOptVecs struct {
	method  vector.Any
	headers vector.Any
	body    vector.Any
}

func NewRobot(rctx *runtime.Context, env *exec.Environment, parent vio.Puller, e expr.Evaluator, format string, method, headers, body expr.Evaluator, p vio.Pushdown, progress *vio.Progress) *Robot {
	return &Robot{
		parent:   parent,
		rctx:     rctx,
		env:      env,
		expr:     e,
		pushdown: p,
		progress: progress,
		format:   format,
		method:   method,
		headers:  headers,
		body:     body,
	}
}

func (o *Robot) isHTTP() bool {
	return o.method != nil || o.headers != nil || o.body != nil
}

func (o *Robot) Pull(done bool) (vector.Any, error) {
	if done {
		o.off = 0
		o.vec = nil
		o.optVecs = httpOptVecs{}
		src := o.src
		o.src = nil
		var err error
		if src != nil {
			_, err = src.Pull(true)
		}
		if _, pullErr := o.parent.Pull(true); err == nil {
			err = pullErr
		}
		return nil, err
	}
	return o.pullNext()
}

func (o *Robot) pullNext() (vector.Any, error) {
	for {
		puller := o.src
		if puller == nil {
			var err error
			puller, err = o.getPuller()
			if puller == nil || err != nil {
				return nil, err
			}
		}
		b, err := puller.Pull(false)
		if b != nil {
			return b, err
		}
		o.src = nil
		if err != nil {
			return nil, err
		}
		_, err = puller.Pull(true)
		if err != nil {
			return nil, err
		}
	}
}

func (o *Robot) getPuller() (vio.Puller, error) {
	src, err := o.nextPuller()
	o.src = src
	return src, err
}

func (o *Robot) nextPuller() (vio.Puller, error) {
	vec := o.vec
	if vec != nil && o.off >= vec.Len() {
		o.off = 0
		o.vec = nil
		o.optVecs = httpOptVecs{}
		vec = nil
	}
	if vec == nil {
		var err error
		if vec, err = o.nextVec(); err != nil {
			return nil, err
		}
		o.vec = vec
		o.off = 0
		if vec == nil {
			return nil, nil
		}
	}
	off := o.off
	o.off++
	var b scode.Builder
	val := vector.ValueAt(&b, vec, off)
	if !val.IsString() {
		return o.errOnVal("from encountered non-string input", vector.Pick(vec, []uint32{off})), nil
	}
	return o.open(val.AsString(), off)
}

func (o *Robot) errOnVal(msg string, vec vector.Any) vio.Puller {
	out := vector.NewWrappedError(o.rctx.Sctx, msg, vec)
	return vio.NewPuller(out)
}

func (o *Robot) nextVec() (vector.Any, error) {
	vec, err := o.parent.Pull(false)
	if err != nil {
		return nil, err
	}
	if vec == nil {
		o.vec = nil
		o.optVecs = httpOptVecs{}
		o.off = 0
		return nil, nil
	}
	o.optVecs = httpOptVecs{
		method:  evalOpt(o.method, vec),
		headers: evalOpt(o.headers, vec),
		body:    evalOpt(o.body, vec),
	}
	vec = o.expr.Eval(vec)
	o.vec = vec
	o.off = 0
	return vec, nil
}

// open opens path, the URL computed for the value in slot of the current
// input vector.
func (o *Robot) open(path string, slot uint32) (vio.Puller, error) {
	format := o.format
	if format == "" {
		format = anyio.ReadFormatFromPath(path)
	}
	if o.isHTTP() {
		return o.openHTTP(path, format, slot)
	}
	if o.env.IsAttached() {
		// XXX we should support committish values
		id, err := o.env.TableID(o.rctx, path)
		if err == nil {
			return o.openTable(id)
		}
	}
	return o.env.Open(o.rctx.Context, o.rctx.Sctx, path, format, o.pushdown, o.progress, 1)
}

func (r *Robot) openTable(id uuid.UUID) (vio.Puller, error) {
	ctx := r.rctx.Context
	table, err := r.env.DB().OpenTable(ctx, id)
	if err != nil {
		return nil, err
	}
	branch, err := r.env.DB().CommitObject(ctx, id, "main")
	if err != nil {
		return nil, err
	}
	l, err := NewLister(ctx, r.rctx.Sctx, table, branch, nil)
	if err != nil {
		return nil, err
	}
	// XXX We're passing nil for pushdown here.  In a subsequent PR, we will
	// work out vector pushdown and put this back.
	return NewTableScanner(r.rctx, l, table, nil, nil, nil), nil
}

// openHTTP opens path with the HTTP options of the value in slot of the
// current input vector.
func (o *Robot) openHTTP(path, format string, slot uint32) (vio.Puller, error) {
	if u, err := url.Parse(path); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return o.errOnVal("from: method, headers, and body require an http or https URL", vector.Pick(o.vec, []uint32{slot})), nil
	}
	var method string
	if vec := o.optVecs.method; vec != nil {
		var b scode.Builder
		var err error
		if method, err = exec.HTTPMethod(vector.ValueAt(&b, vec, slot)); err != nil {
			return o.errOnVal("from: "+err.Error(), vector.Pick(vec, []uint32{slot})), nil
		}
	}
	var headers map[string][]string
	if vec := o.optVecs.headers; vec != nil {
		var b scode.Builder
		var err error
		if headers, err = exec.HTTPHeaders(vector.ValueAt(&b, vec, slot)); err != nil {
			return o.errOnVal("from: "+err.Error(), vector.Pick(vec, []uint32{slot})), nil
		}
	}
	var body io.Reader
	if vec := o.optVecs.body; vec != nil {
		var b scode.Builder
		bodyBytes, err := exec.HTTPBody(vector.ValueAt(&b, vec, slot))
		if err != nil {
			return o.errOnVal("from: "+err.Error(), vector.Pick(vec, []uint32{slot})), nil
		}
		body = bytes.NewReader(bodyBytes)
	}
	return o.env.OpenHTTP(o.rctx.Context, o.rctx.Sctx, path, format, method, headers, body, o.pushdown)
}

func evalOpt(e expr.Evaluator, vec vector.Any) vector.Any {
	if e == nil {
		return nil
	}
	return e.Eval(vec)
}
