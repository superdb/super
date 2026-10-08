package op

import (
	"fmt"

	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Wrapper struct {
	Parent vio.Puller
	Msg    string
}

func (w *Wrapper) Pull(done bool) (vector.Any, error) {
	fmt.Printf("%p %T %s %v\n", w.Parent, w.Parent, w.Msg, done)
	vec, err := w.Parent.Pull(done)
	fmt.Printf("%p %T %s %v %v %v\n", w.Parent, w.Parent, w.Msg, done, vec, err)
	return vec, err
}
