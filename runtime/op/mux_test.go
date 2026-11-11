package op

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// pullAll pulls mux until EOS and returns the number of values with each
// label and the labels of the channel ends.  It fails if a value follows the
// end of its channel.
func pullAll(t *testing.T, mux *Mux) (map[string]int, []string) {
	counts := map[string]int{}
	var ends []string
	for {
		vec, err := mux.Pull(false)
		require.NoError(t, err)
		if vec == nil {
			return counts, ends
		}
		labeled := vec.(*vector.Labeled)
		if labeled.Any == nil {
			ends = append(ends, labeled.Label)
			continue
		}
		require.NotContains(t, ends, labeled.Label)
		counts[labeled.Label]++
	}
}

// sender sends a vector with each of labels from inside Pull, as an output
// operator in a subquery or a debug operator does, and then returns vec once.
type sender struct {
	ctx     context.Context
	outputs *Outputs
	labels  []string
	vec     vector.Any
	done    bool
}

func (s *sender) Pull(bool) (vector.Any, error) {
	if s.done {
		return nil, nil
	}
	s.done = true
	for _, label := range s.labels {
		if err := s.outputs.send(s.ctx, label, vector.NewNull(1)); err != nil {
			return nil, err
		}
	}
	return s.vec, nil
}

func TestMux(t *testing.T) {
	rctx := runtime.NewContext(context.Background(), super.NewContext())
	outputs := NewOutputs()
	null := vector.NewNull(1)
	// Two leaves output to main and one to foo, and a subquery in the
	// second leaf outputs to main and bar and debugs.
	subquery := &sender{
		ctx:     rctx,
		outputs: outputs,
		labels:  []string{"main", "bar", "debug", "main"},
		vec:     null,
	}
	NewOutput(rctx, nil, outputs, "bar")
	leaves := []vio.Puller{
		NewOutput(rctx, vio.NewPuller(null, null), outputs, "main"),
		NewOutput(rctx, subquery, outputs, "main"),
		NewOutput(rctx, vio.NewPuller(null), outputs, "foo"),
	}
	counts, ends := pullAll(t, NewMux(rctx, outputs, leaves))
	require.Equal(t, map[string]int{"main": 5, "foo": 1, "bar": 1, "debug": 1}, counts)
	// The channels end in order and there's no end of channel for debug.
	require.Equal(t, []string{"bar", "foo", "main"}, ends)
	require.Error(t, rctx.Err())
}

type errPuller struct{}

func (errPuller) Pull(bool) (vector.Any, error) {
	return nil, errors.New("leaf failed")
}

func TestMuxError(t *testing.T) {
	rctx := runtime.NewContext(context.Background(), super.NewContext())
	outputs := NewOutputs()
	leaves := []vio.Puller{NewOutput(rctx, errPuller{}, outputs, "main")}
	_, err := NewMux(rctx, outputs, leaves).Pull(false)
	require.EqualError(t, err, "leaf failed")
	require.Error(t, rctx.Err())
}

func TestOutputsNil(t *testing.T) {
	// A nil Outputs discards its values, as at compile time.
	rctx := runtime.NewContext(context.Background(), super.NewContext())
	out := NewOutput(rctx, vio.NewPuller(vector.NewNull(1)), nil, "main")
	vec, err := out.Pull(false)
	require.NoError(t, err)
	require.Nil(t, vec)
}
