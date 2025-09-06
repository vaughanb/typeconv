package typeconv

import (
	"reflect"
)

type dynamicPlan struct {
	steps []step
	opts  Options
}

func buildDynamicPlan(st, dt reflect.Type, opts Options) (*dynamicPlan, error) {
	smap := getFieldMap(st, opts.Tag)
	dmap := getFieldMap(dt, opts.Tag)
	var steps []step
	for name, sfi := range smap {
		if dfi, ok := dmap[name]; ok {
			stLeaf := st.FieldByIndex(sfi).Type
			dtLeaf := dt.FieldByIndex(dfi).Type
			steps = append(steps, step{srcIndex: sfi, dstIndex: dfi, srcType: stLeaf, dstType: dtLeaf})
		}
	}
	if len(steps) == 0 {
		return nil, errNoOverlappingJSONTaggedFields
	}
	return &dynamicPlan{steps: steps, opts: opts}, nil
}

func (p *dynamicPlan) run(dst, src reflect.Value, reg localConverterRegistry) error {
	for _, s := range p.steps {
		conv, err := makeLeafConv(s.srcType, s.dstType, p.opts, reg)
		if err != nil {
			return err
		}
		if err := conv(dst.FieldByIndex(s.dstIndex), src.FieldByIndex(s.srcIndex)); err != nil {
			return err
		}
	}
	return nil
}
