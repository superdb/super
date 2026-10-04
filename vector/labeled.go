package vector

type Labeled struct {
	Any
	Label string
}

func Unlabel(vec Any) (Any, string) {
	if vec, ok := vec.(*Labeled); ok {
		return vec.Any, vec.Label
	}
	return vec, ""
}

// EOS is indicated with a label-wrapped nil but we
// would still like Len to work so this is here.
func (l *Labeled) Len() uint32 {
	if l.Any == nil {
		return 0
	}
	return l.Any.Len()
}

type Control struct {
	Any
}
