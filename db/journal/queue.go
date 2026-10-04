package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup/oldbsup"
	"github.com/superdb/super/pkg/storage"
)

const ext = "bsuprows"

var (
	ErrEmpty  = errors.New("empty log")
	ErrFailed = errors.New("transaction failed")
)

type ID uint64

const Nil ID = 0

const MaxReadRetry = 10

type Queue struct {
	engine       storage.Engine
	path         *storage.URI
	headPath     *storage.URI
	tailPath     *storage.URI
	tailLockPath *storage.URI
}

func New(engine storage.Engine, path *storage.URI) *Queue {
	return &Queue{
		engine:       engine,
		path:         path,
		headPath:     path.JoinPath("HEAD"),
		tailPath:     path.JoinPath("TAIL"),
		tailLockPath: path.JoinPath("TAIL.lock"),
	}
}

func (q *Queue) Path() *storage.URI {
	return q.path
}

func (q *Queue) ReadHead(ctx context.Context) (ID, error) {
	//XXX The head file can be wrong due to races but it will always be
	// close so we should probe for the next slot(s) and update the HEAD
	// object if we find a hit.  See issue #XXX.
	id, _, err := readID(ctx, q.engine, q.headPath)
	return id, err
}

func (q *Queue) writeHead(ctx context.Context, id ID) error {
	return writeID(ctx, q.engine, q.headPath, id)
}

func (q *Queue) ReadTail(ctx context.Context) (ID, ID, error) {
	return readID(ctx, q.engine, q.tailPath)
}

func (q *Queue) writeTail(ctx context.Context, id, base ID) error {
	r := strings.NewReader(fmt.Sprintf("%d %d", id, base))
	return storage.Put(ctx, q.engine, q.tailPath, r)
}

// MoveTail moves the tail of the journal to the indicated ID and does
// no validation.  Use with caution.  This update must be made by an
// exclusive write-lock that is outside the scope of the journal package.
// Unlike HEAD, TAIL is not a hint and must be consistent with the actual
// log entries at all times.
func (q *Queue) MoveTail(ctx context.Context, id, base ID) error {
	return q.writeTail(ctx, id, base)
}

func (q *Queue) Boundaries(ctx context.Context) (ID, ID, ID, error) {
	head, err := q.ReadHead(ctx)
	if err != nil {
		return Nil, Nil, Nil, err
	}
	tail, base, err := q.ReadTail(ctx)
	if err != nil {
		return Nil, Nil, Nil, err
	}
	return head, tail, base, nil
}

// XXX This needs concurrency work. See issue #2546.
func (q *Queue) Commit(ctx context.Context, b []byte) (ID, error) {
	head, err := q.ReadHead(ctx)
	if err != nil {
		return Nil, err
	}
	if err := q.CommitAt(ctx, head, b); err != nil {
		return Nil, err
	}
	return head + 1, err
}

// CommitAt commits a new serialized BSUP sequence to the journal presuming
// the previous state conformed to the journal position "at".  The entry is
// written at the next position in the log if possible.  Otherwise, a write
// conflict occurs and an error is returned.
func (q *Queue) CommitAt(ctx context.Context, at ID, b []byte) error {
	uri := q.uri(at + 1)
	if err := q.engine.PutIfNotExists(ctx, uri, b); err != nil {
		if err != storage.ErrNotSupported {
			return err
		}
		//XXX Here, we need to emulate PutIfNotExists using S3's
		// strong ordering guarantees.  Currently, this is incorrect
		// and can race with multiple writers.  See issue #2686.
		w, err := q.engine.Put(ctx, uri)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, bytes.NewReader(b))
		if err != nil {
			w.Close()
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
	}
	return q.writeHead(ctx, at+1)
}

// NewReader returns a BSUP reader that concatenates the journal files
// in sequence from tail to head.  Since BSUP is stored in the journal,
// concatenation produce valid BSUP.
func (q *Queue) NewReader(ctx context.Context, head, tail ID) *Reader {
	return newReader(ctx, q, head, tail)
}

func (q *Queue) uri(id ID) *storage.URI {
	return q.path.JoinPath(fmt.Sprintf("%d.%s", id, ext))
}

func (q *Queue) Load(ctx context.Context, id ID) ([]byte, error) {
	return storage.Get(ctx, q.engine, q.uri(id))
}

func (q *Queue) DeleteCommit(ctx context.Context, id ID) error {
	return q.engine.Delete(ctx, q.uri(id))
}

func (q *Queue) Open(ctx context.Context, head, tail ID) (io.Reader, error) {
	if head == Nil {
		var err error
		head, err = q.ReadHead(ctx)
		if err != nil {
			return nil, err
		}
		if head == Nil {
			// Return an empty reader when the journal is empty.
			// This is preferred over returning ErrEmpty and
			// havings layers above report the error message instead
			// of simply processing an empty input without error.
			return strings.NewReader(""), nil
		}
	}
	if tail == Nil {
		var err error
		tail, _, err = q.ReadTail(ctx)
		if err != nil {
			return nil, err
		}
	}
	return q.NewReader(ctx, head, tail), nil
}

func (q *Queue) putTailLockFile(ctx context.Context) error {
	return q.engine.PutIfNotExists(ctx, q.tailLockPath, nil)
}

func (q *Queue) deleteTailLockFile() error {
	return q.engine.Delete(context.Background(), q.tailLockPath)
}

func (q *Queue) OpenAsBSUPRows(ctx context.Context, sctx *super.Context, head, tail ID) (*oldbsup.Reader, error) {
	r, err := q.Open(ctx, head, tail)
	if err != nil {
		return nil, err
	}
	return oldbsup.NewReader(sctx, r), nil
}

func writeID(ctx context.Context, engine storage.Engine, u *storage.URI, id ID) error {
	r := strings.NewReader(strconv.FormatUint(uint64(id), 10))
	return storage.Put(ctx, engine, u, r)
}

func readID(ctx context.Context, engine storage.Engine, path *storage.URI) (ID, ID, error) {
	var retry int
	timeout := time.Millisecond
	for {
		b, err := storage.Get(ctx, engine, path)
		if err != nil {
			return Nil, Nil, err
		}
		list := strings.Split(string(b), " ")
		if id, err := strconv.ParseUint(list[0], 10, 64); err == nil {
			if len(list) == 1 {
				return ID(id), Nil, nil
			}
			if base, err := strconv.ParseUint(list[1], 10, 64); err == nil {
				return ID(id), ID(base), nil
			}
		}
		retry++
		if retry > MaxReadRetry || timeout > 5*time.Second {
			return Nil, Nil, fmt.Errorf("can read but not parse contents of journal HEAD: %s", b)
		}
		select {
		case <-time.After(timeout):
		case <-ctx.Done():
			return Nil, Nil, ctx.Err()
		}
		t := 2 * int(timeout)
		timeout = time.Duration(t + rand.Intn(t))
	}
}

func Create(ctx context.Context, engine storage.Engine, path *storage.URI, base ID) (*Queue, error) {
	q := New(engine, path)
	if err := q.writeHead(ctx, Nil); err != nil {
		return nil, err
	}
	// Tail is initialized with the first entry of the journal, which does
	// not yet exist.  The journal is empty iff HEAD == 0.  Once written
	// to the journal is never empty again, but it may be reset by higher
	// layers by writing a NOP and moving TAIL to HEAD.
	if err := q.writeTail(ctx, 1, base); err != nil {
		return nil, err
	}
	return q, nil
}

func Open(ctx context.Context, engine storage.Engine, path *storage.URI) (*Queue, error) {
	q := New(engine, path)
	if _, err := q.ReadHead(ctx); err != nil {
		return nil, fmt.Errorf("%s: no such journal", path)
	}
	return q, nil
}
