package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type CIDRMatch struct {
	sctx *super.Context
	pw   *expr.PredicateWalk
}

func NewCIDRMatch(sctx *super.Context) *CIDRMatch {
	return &CIDRMatch{sctx, expr.NewPredicateWalk(sctx, cidrMatch)}
}

func (c *CIDRMatch) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(c.sctx, true, args, "cidr_match"); ok {
		return vec
	}
	if args[0].Type().ID() != super.IDNet {
		return vector.NewWrappedError(c.sctx, "cidr_match: not a net", args[0])
	}
	return c.pw.Eval(args...)
}

func cidrMatch(vec ...vector.Any) vector.Any {
	netVec, valVec := vec[0], vec[1]
	if id := valVec.Type().ID(); id != super.IDIP {
		return vector.NewConstBool(false, valVec.Len())
	}
	out := vector.NewFalse(valVec.Len())
	for i := range netVec.Len() {
		net := vector.NetValue(netVec, i)
		ip := vector.IPValue(valVec, i)
		if net.Contains(ip) {
			out.Set(i)
		}
	}
	return out
}
