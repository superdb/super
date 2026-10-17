package function

import (
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/grok"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Grok struct {
	sctx    *super.Context
	builder scode.Builder
	hosts   map[string]*host
	// fields is used as a scratch space to avoid allocating a new slice.
	fields []super.Field
}

func newGrok(sctx *super.Context) *Grok {
	return &Grok{
		sctx:  sctx,
		hosts: make(map[string]*host),
	}
}

func (g *Grok) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(g.sctx, true, args, "grok"); ok {
		return vec
	}
	patternArg, inputArg := args[0], args[1]
	var defArg vector.Any
	if len(args) > 2 {
		defArg = args[2]
	} else {
		defArg = vector.NewConstString("", args[0].Len())
	}
	switch {
	case super.TypeUnder(defArg.Type()) != super.TypeString:
		return g.error("definitions argument must be a string", defArg)
	case super.TypeUnder(patternArg.Type()) != super.TypeString:
		return g.error("pattern argument must be a string", patternArg)
	case super.TypeUnder(inputArg.Type()) != super.TypeString:
		return g.error("input argument must be a string", inputArg)
	}
	builder := vector.NewDynamicValueBuilder()
	var defErrs, patErrs []string
	var defErrsIdx, patErrsIdx, inErrsIdx []uint32
	for i := range patternArg.Len() {
		def := vector.StringValue(defArg, i)
		h, err := g.getHost(def)
		if err != nil {
			defErrs = append(defErrs, err.Error())
			defErrsIdx = append(defErrsIdx, i)
			continue
		}
		pat := vector.StringValue(patternArg, i)
		p, err := h.getPattern(pat)
		if err != nil {
			patErrs = append(patErrs, err.Error())
			patErrsIdx = append(patErrsIdx, i)
			continue
		}
		in := vector.StringValue(inputArg, i)
		keys, vals, match := p.ParseKeyValues(in)
		if !match {
			inErrsIdx = append(inErrsIdx, i)
			continue
		}
		g.fields = g.fields[:0]
		for _, key := range keys {
			g.fields = append(g.fields, super.NewField(key, super.TypeString))
		}
		typ := g.sctx.MustLookupTypeRecord(g.fields)
		g.builder.Reset()
		if len(vals) == 0 {
			// If we have a match but no key/vals return empty record.
			g.builder.Append(nil)
		} else {
			for _, s := range vals {
				g.builder.Append([]byte(s))
			}
		}
		builder.Write(super.NewValue(typ, g.builder.Bytes()))
	}
	combiner := vector.NewCombiner(builder.Build(g.sctx))
	if len(defErrsIdx) > 0 {
		combiner.Add(defErrsIdx, g.errorVec(defErrs, defErrsIdx, defArg))
	}
	if len(patErrsIdx) > 0 {
		combiner.Add(patErrsIdx, g.errorVec(patErrs, patErrsIdx, patternArg))
	}
	if len(inErrsIdx) > 0 {
		combiner.Add(inErrsIdx, g.error("value does not match pattern", vector.Pick(inputArg, inErrsIdx)))
	}
	return combiner.Result()
}

func (g *Grok) errorVec(msgs []string, index []uint32, vec vector.Any) vector.Any {
	s := vector.NewStringEmpty(0)
	for _, m := range msgs {
		s.Append("grok: " + m)
	}
	return vector.NewVecWrappedError(g.sctx, s, vector.Pick(vec, index))
}

func (g *Grok) error(msg string, vec vector.Any) vector.Any {
	return vector.NewWrappedError(g.sctx, "grok: "+msg, vec)
}

func (g *Grok) getHost(defs string) (*host, error) {
	h, ok := g.hosts[defs]
	if !ok {
		h = &host{Host: grok.NewBase(), patterns: make(map[string]*grok.Pattern)}
		if err := h.AddFromReader(strings.NewReader(defs)); err != nil {
			return nil, err
		}
		g.hosts[defs] = h
	}
	return h, nil
}

type host struct {
	grok.Host
	patterns map[string]*grok.Pattern
}

func (h *host) getPattern(patternArg string) (*grok.Pattern, error) {
	p, ok := h.patterns[patternArg]
	if !ok {
		var err error
		p, err = h.Host.Compile(patternArg)
		if err != nil {
			return nil, err
		}
		h.patterns[patternArg] = p
	}
	return p, nil
}
