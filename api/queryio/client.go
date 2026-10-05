package queryio

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type scanner struct {
	sctx     *super.Context
	channel  string
	scanner  vio.Scanner
	closer   io.Closer
	progress vio.Progress
}

func NewScanner(ctx context.Context, rc io.ReadCloser) (vio.Scanner, error) {
	sctx := super.NewContext()
	s, err := bsupio.NewReader(ctx, sctx, rc, nil, 1)
	if err != nil {
		return nil, err
	}
	return &scanner{
		sctx:    sctx,
		scanner: s,
		closer:  rc,
	}, nil
}

func (s *scanner) Progress() vio.Progress {
	return s.progress
}

func (s *scanner) Pull(done bool) (vector.Any, error) {
again:
	vec, err := s.scanner.Pull(done)
	if vec == nil || err != nil {
		if closeErr := s.closer.Close(); err == nil {
			err = closeErr
		}
		return nil, err
	}
	vctrl, ok := vec.(*vector.Control)
	if !ok {
		return &vector.Labeled{Any: vec, Label: s.channel}, nil
	}
	ctrl, err := unmarshalControl(vctrl)
	if err != nil {
		return nil, err
	}
	switch ctrl := ctrl.(type) {
	case *api.QueryChannelSet:
		s.channel = ctrl.Channel
		goto again
	case *api.QueryChannelEnd:
		return &vector.Labeled{Label: ctrl.Channel}, nil
	case *api.QueryStats:
		s.progress.Add(ctrl.Progress)
		goto again
	case *api.QueryError:
		return nil, errors.New(ctrl.Error)
	default:
		return nil, fmt.Errorf("unsupported control message: %T", ctrl)
	}
}

func unmarshalControl(vec vector.Any) (any, error) {
	if vec.Len() != 1 {
		return nil, fmt.Errorf("control vector length is %d, not 1", vec.Len())
	}
	val := vector.ValueAt(nil, vec, 0)
	var v any
	if err := unmarshaler.Unmarshal(val, &v); err != nil {
		return nil, fmt.Errorf("unable to unmarshal control message: %w ", err)
	}
	return v, nil
}
