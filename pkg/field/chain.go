package field

type ChainElem struct {
	ID      string
	Noneish bool
	Nullish bool
}

type Chain []ChainElem

func NewChain(ids ...string) Chain {
	chain := make([]ChainElem, 0, len(ids))
	for _, id := range ids {
		chain = append(chain, ChainElem{ID: id})
	}
	return chain
}

func NewChainNullish(ids ...string) Chain {
	chain := make([]ChainElem, 0, len(ids))
	for _, id := range ids {
		chain = append(chain, ChainElem{ID: id, Nullish: true})
	}
	return chain
}

// Append appends id to c.  The new element is nullish if the last element of
// c is, so a field access on a nullish chain (e.g., c in a.b.c, where a.b is
// a SQL column reference) is nullish too.
func (c Chain) Append(id string, noneish bool) Chain {
	nullish := len(c) > 0 && c[len(c)-1].Nullish
	return append(c, ChainElem{id, noneish, nullish})
}

func (c Chain) AppendNullish(id string) Chain {
	return append(c, ChainElem{id, false, true})
}

func (c Chain) Path() Path {
	path := make([]string, 0, len(c))
	for _, elem := range c {
		path = append(path, elem.ID)
	}
	return path
}

func (c Chain) String() string {
	return c.Path().String()
}
