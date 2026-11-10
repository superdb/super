package loader

import (
	"sync"

	"github.com/superdb/super/pkg/byteconv"
)

type collector struct {
	loader  *FrameLoader
	wg      sync.WaitGroup
	mu      sync.Mutex
	strings []string
}

func (c *collector) append(b []byte) {
	if len(b) == 0 {
		return
	}
	s := byteconv.UnsafeString(b)
	c.mu.Lock()
	c.strings = append(c.strings, s)
	c.mu.Unlock()
}

func (c *collector) spawn(sh Shadow) {
	c.wg.Go(func() { sh.strings(c) })
}

func (r *Record) strings(c *collector) {
	for _, f := range r.Fields {
		c.spawn(f)
	}
}

func (d *Dynamic) strings(c *collector) {
	for _, v := range d.Values {
		c.spawn(v)
	}
}

func (u *Union) strings(c *collector) {
	for _, v := range u.Values {
		c.spawn(v)
	}
}

func (m *Map) strings(c *collector) {
	c.spawn(m.Keys)
	c.spawn(m.Values)
}

func (f *Fusion) strings(c *collector) { f.Values.strings(c) }
func (a *Array) strings(c *collector)  { a.Values.strings(c) }
func (o *Option) strings(c *collector) { o.Values.strings(c) }
func (n *Named) strings(c *collector)  { n.Values.strings(c) }
func (e *Error) strings(c *collector)  { e.Values.strings(c) }
func (d *Dict) strings(c *collector)   { d.Values.strings(c) }

func (*Int) strings(*collector)       {}
func (*Uint) strings(*collector)      {}
func (*Float) strings(*collector)     {}
func (*Bool) strings(*collector)      {}
func (*IP) strings(*collector)        {}
func (*Net) strings(*collector)       {}
func (*Null) strings(*collector)      {}
func (*None) strings(*collector)      {}
func (*TypeValue) strings(*collector) {}
func (*Enum) strings(*collector)      {}
