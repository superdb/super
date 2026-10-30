package function

import (
	"github.com/superdb/super/vector"
)

type superDefuse struct{}

func (superDefuse) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

func (superDefuse) Call(args ...vector.Any) vector.Any {
	return args[0]
}
