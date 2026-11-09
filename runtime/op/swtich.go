package op

import (
	"context"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Switch struct {
	router *router
	cases  []expr.Evaluator
}

func NewSwitch(ctx context.Context, parent vio.Puller) *Switch {
	s := &Switch{}
	s.router = newRouter(ctx, s, parent)
	return s
}

func (s *Switch) AddCase(e expr.Evaluator) vio.Puller {
	s.cases = append(s.cases, e)
	return s.router.addRoute()
}

func (s *Switch) forward(vec vector.Any) bool {
	doneMap := roaring.New()
	for i, c := range s.cases {
		maskVec := c.Eval(vec)
		boolMap, errMap := expr.BoolMask(maskVec)
		boolMap.AndNot(doneMap)
		errMap.AndNot(doneMap)
		doneMap.Or(boolMap)
		var vec2 vector.Any
		if errMap.IsEmpty() {
			if boolMap.IsEmpty() {
				continue
			}
			vec2 = vector.Pick(vec, boolMap.ToArray())
		} else if boolMap.IsEmpty() {
			vec2 = vector.Pick(maskVec, errMap.ToArray())
		} else {
			valIndex := boolMap.ToArray()
			errIndex := errMap.ToArray()
			tags := make([]uint32, 0, len(valIndex)+len(errIndex))
			var j, k int
			for j < len(valIndex) && k < len(errIndex) {
				if valIndex[j] < errIndex[k] {
					j++
					tags = append(tags, 0)
				} else {
					k++
					tags = append(tags, 1)
				}
			}
			for ; j < len(valIndex); j++ {
				tags = append(tags, 0)
			}
			for ; k < len(errIndex); k++ {
				tags = append(tags, 1)
			}
			valVec := vector.Pick(vec, valIndex)
			errVec := vector.Pick(maskVec, errIndex)
			vec2 = vector.Stitch(vector.NewDynamic(tags, []vector.Any{valVec, errVec}))
		}
		if !s.router.routes[i].send(vec2, nil) {
			return false
		}
	}
	return true
}
