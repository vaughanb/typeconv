package typeconv

import (
	"reflect"
	"sync"
)

type pair struct {
	st, dt reflect.Type
	strict bool
	tag    string
}

var planCache sync.Map // map[pair]*Plan[_,_]

func loadPlan[S any, D any](k pair) *Plan[S, D] {
	if v, ok := planCache.Load(k); ok {
		if p, ok2 := v.(*Plan[S, D]); ok2 {
			return p
		}
	}
	return nil
}

func savePlan[S any, D any](k pair, p *Plan[S, D]) { planCache.Store(k, p) }
