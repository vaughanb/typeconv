package typeconv

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	defaultOptions                   = Options{Tag: "json"}
	errNoOverlappingJSONTaggedFields = errors.New("no overlapping JSON-tagged fields")
)

type Options struct {
	Tag         string
	StrictTypes bool
}

type convKey struct {
	src reflect.Type
	dst reflect.Type
}

// localConverterRegistry is built per Convert call from user-provided functions.
// It maps a pair of concrete (non-pointer) types to a leaf converter.
type localConverterRegistry map[convKey]leafConv

// Convert copies data from src to dst using a cached plan inferred from JSON tags.
// Argument order: src, dst, [customConverters].
func Convert[S any, D any](src *S, dst *D, customConverters ...any) error {
	p, err := BuildPlan[S, D](defaultOptions)
	if err != nil {
		return err
	}
	reg, err := buildLocalRegistry(customConverters)
	if err != nil {
		return err
	}
	return p.convertWithRegistry(dst, src, reg)
}

// buildLocalRegistry validates and adapts user-provided converter functions into leaf converters.
func buildLocalRegistry(custom []any) (localConverterRegistry, error) {
	reg := make(localConverterRegistry)
	for _, c := range custom {
		rv := reflect.ValueOf(c)
		if rv.Kind() != reflect.Func {
			return nil, fmt.Errorf("custom converter must be func, got %T", c)
		}
		rt := rv.Type()
		if rt.NumIn() != 2 || rt.In(0).Kind() != reflect.Pointer || rt.In(1).Kind() != reflect.Pointer {
			return nil, fmt.Errorf("converter must be func(*Src, *Dst) error, got %s", rt.String())
		}
		if rt.NumOut() != 1 || !rt.Out(0).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			return nil, fmt.Errorf("converter must return error, got %s", rt.String())
		}
		srcPtr := rt.In(0)
		dstPtr := rt.In(1)
		src := srcPtr.Elem()
		dst := dstPtr.Elem()
		key := convKey{src: src, dst: dst}
		if _, exists := reg[key]; exists {
			return nil, fmt.Errorf("duplicate converter for %s -> %s", src.String(), dst.String())
		}
		reg[key] = func(dstV, srcV reflect.Value) error {
			for srcV.Kind() == reflect.Pointer {
				if srcV.IsNil() {
					dstV.SetZero()
					return nil
				}
				srcV = srcV.Elem()
			}
			for dstV.Kind() == reflect.Pointer {
				if dstV.IsNil() {
					dstV.Set(reflect.New(dstV.Type().Elem()))
				}
				dstV = dstV.Elem()
			}
			args := []reflect.Value{srcV.Addr(), dstV.Addr()}
			out := rv.Call(args)
			if e := out[0].Interface(); e != nil {
				return e.(error)
			}
			return nil
		}
	}
	return reg, nil
}

// Plan represents a compiled conversion plan between two types S and D.
type Plan[S any, D any] struct {
	steps []step
	opts  Options
}

type step struct {
	srcIndex []int
	dstIndex []int
	srcType  reflect.Type
	dstType  reflect.Type
}

type leafConv func(dst, src reflect.Value) error

// BuildPlan creates a conversion plan between types S and D based on the provided options.
func BuildPlan[S any, D any](opts Options) (*Plan[S, D], error) {
	if opts.Tag == "" {
		opts.Tag = "json"
	}
	st := reflect.TypeOf((*S)(nil)).Elem()
	dt := reflect.TypeOf((*D)(nil)).Elem()
	key := pair{st, dt, opts.StrictTypes, opts.Tag}
	if p := loadPlan[S, D](key); p != nil {
		return p, nil
	}

	smap := getFieldMap(st, opts.Tag)
	dmap := getFieldMap(dt, opts.Tag)
	if len(smap) == 0 || len(dmap) == 0 {
		return nil, fmt.Errorf("no mappable fields for tag %q", opts.Tag)
	}

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
	p := &Plan[S, D]{steps: steps, opts: opts}
	savePlan(key, p)
	return p, nil
}

// Convert applies the conversion plan to copy data from src to dst.
func (p *Plan[S, D]) Convert(dst *D, src *S) error {
	if dst == nil || src == nil {
		return errors.New("dst and src must be non-nil pointers")
	}
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for _, s := range p.steps {
		svLeaf := sv.FieldByIndex(s.srcIndex)
		dvLeaf := dv.FieldByIndex(s.dstIndex)
		if !dvLeaf.CanSet() {
			return fmt.Errorf("destination field not settable at %v", s.dstIndex)
		}
		conv, err := makeLeafConv(s.srcType, s.dstType, p.opts, nil)
		if err != nil {
			return err
		}
		if err := conv(dvLeaf, svLeaf); err != nil {
			return err
		}
	}
	return nil
}

// convertWithRegistry is like Convert but uses a provided local registry
// of custom converters for this call.
func (p *Plan[S, D]) convertWithRegistry(dst *D, src *S, reg localConverterRegistry) error {
	if dst == nil || src == nil {
		return errors.New("dst and src must be non-nil pointers")
	}
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for _, s := range p.steps {
		svLeaf := sv.FieldByIndex(s.srcIndex)
		dvLeaf := dv.FieldByIndex(s.dstIndex)
		if !dvLeaf.CanSet() {
			return fmt.Errorf("destination field not settable at %v", s.dstIndex)
		}
		conv, err := makeLeafConv(s.srcType, s.dstType, p.opts, reg)
		if err != nil {
			return err
		}
		if err := conv(dvLeaf, svLeaf); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- Field discovery ----------------
func buildFieldMap(t reflect.Type, tag string) map[string][]int {
	m := map[string][]int{}
	seen := map[string]bool{}
	var walk func(rt reflect.Type, path []int)
	walk = func(rt reflect.Type, path []int) {
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" {
				continue
			}
			name, skip := tagName(f, tag)
			if skip {
				continue
			}
			idx := append(append([]int{}, path...), i)
			if name == "" {
				if f.Anonymous && isStructLike(f.Type) {
					walk(f.Type, idx)
					continue
				}
				name = f.Name
			}
			key := strings.ToLower(name)
			if !seen[key] {
				m[key] = idx
				seen[key] = true
			}
		}
	}
	walk(t, nil)
	return m
}

// Cached field maps to avoid repeated reflection over struct fields
type fieldMapKey struct {
	t   reflect.Type
	tag string
}

var fieldMapCache sync.Map // map[fieldMapKey]map[string][]int

func getFieldMap(t reflect.Type, tag string) map[string][]int {
	key := fieldMapKey{t: t, tag: tag}
	if v, ok := fieldMapCache.Load(key); ok {
		return v.(map[string][]int)
	}
	m := buildFieldMap(t, tag)
	fieldMapCache.Store(key, m)
	return m
}

func tagName(f reflect.StructField, tag string) (name string, skip bool) {
	if tv, ok := f.Tag.Lookup(tag); ok {
		if tv == "-" {
			return "", true
		}
		parts := strings.Split(tv, ",")
		if parts[0] != "" {
			return parts[0], false
		}
	}
	return "", false
}

func isStructLike(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

// ---------------- Converters ----------------
func makeLeafConv(st, dt reflect.Type, opts Options, reg localConverterRegistry) (leafConv, error) {
	// 1. Direct types
	if dt == st || dt.AssignableTo(st) || st.AssignableTo(dt) {
		return assignConv(st, dt), nil
	}

	// 2. Per-call custom converter
	if reg != nil {
		if cv, ok := reg[convKey{st, dt}]; ok {
			return cv, nil
		}
	}

	// 3. Struct recursion
	if isStructLike(st) && isStructLike(dt) {
		return structConv(st, dt, opts, reg), nil
	}

	// 4. Slice
	if st.Kind() == reflect.Slice && dt.Kind() == reflect.Slice {
		elemConv, err := makeLeafConv(st.Elem(), dt.Elem(), opts, reg)
		if err != nil {
			return nil, err
		}
		return sliceConv(elemConv, dt), nil
	}

	// 5. Map[string]T
	if st.Kind() == reflect.Map && dt.Kind() == reflect.Map && st.Key().Kind() == reflect.String && dt.Key().Kind() == reflect.String {
		elemConv, err := makeLeafConv(st.Elem(), dt.Elem(), opts, reg)
		if err != nil {
			return nil, err
		}
		return mapConv(elemConv, dt), nil
	}

	// 6. Convertible
	if !opts.StrictTypes && st.ConvertibleTo(dt) {
		return assignConv(st, dt), nil
	}

	// 7. JSON fallback
	return jsonFallbackConv(st, dt)
}

func assignConv(st, dt reflect.Type) leafConv {
	return func(dst, src reflect.Value) error {
		for src.Kind() == reflect.Pointer {
			if src.IsNil() {
				dst.SetZero()
				return nil
			}
			src = src.Elem()
		}
		for dst.Kind() == reflect.Pointer {
			if dst.IsNil() {
				dst.Set(reflect.New(dst.Type().Elem()))
			}
			dst = dst.Elem()
		}
		dst.Set(src.Convert(dst.Type()))
		return nil
	}
}

func structConv(st, dt reflect.Type, opts Options, reg localConverterRegistry) leafConv {
	// Normalize to non-pointer struct types for planning
	stBase := st
	if stBase.Kind() == reflect.Pointer {
		stBase = stBase.Elem()
	}
	dtBase := dt
	if dtBase.Kind() == reflect.Pointer {
		dtBase = dtBase.Elem()
	}

	// Build once at converter creation time
	dp, err := buildDynamicPlan(stBase, dtBase, opts)
	if err != nil {
		if errors.Is(err, errNoOverlappingJSONTaggedFields) {
			jsonConv, _ := jsonFallbackConv(st, dt)
			return func(dst, src reflect.Value) error {
				return jsonConv(dst, src)
			}
		}
		return func(dst, src reflect.Value) error { return err }
	}
	return func(dst, src reflect.Value) error {
		for src.Kind() == reflect.Pointer {
			if src.IsNil() {
				dst.SetZero()
				return nil
			}
			src = src.Elem()
		}
		for dst.Kind() == reflect.Pointer {
			if dst.IsNil() {
				dst.Set(reflect.New(dst.Type().Elem()))
			}
			dst = dst.Elem()
		}
		return dp.run(dst, src, reg)
	}
}

func sliceConv(elemConv leafConv, dt reflect.Type) leafConv {
	return func(dst, src reflect.Value) error {
		if src.IsNil() {
			dst.SetZero()
			return nil
		}
		ln := src.Len()
		out := reflect.MakeSlice(dt, ln, ln)
		for i := 0; i < ln; i++ {
			if err := elemConv(out.Index(i), src.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(out)
		return nil
	}
}

func mapConv(elemConv leafConv, dt reflect.Type) leafConv {
	return func(dst, src reflect.Value) error {
		if src.IsNil() {
			dst.SetZero()
			return nil
		}
		out := reflect.MakeMapWithSize(dt, src.Len())
		iter := src.MapRange()
		for iter.Next() {
			ov := reflect.New(dt.Elem()).Elem()
			if err := elemConv(ov, iter.Value()); err != nil {
				return err
			}
			out.SetMapIndex(iter.Key().Convert(dt.Key()), ov)
		}
		dst.Set(out)
		return nil
	}
}

func jsonFallbackConv(st, dt reflect.Type) (leafConv, error) {
	return func(dst, src reflect.Value) error {
		for src.Kind() == reflect.Pointer {
			if src.IsNil() {
				dst.SetZero()
				return nil
			}
			src = src.Elem()
		}
		for dst.Kind() == reflect.Pointer {
			if dst.IsNil() {
				dst.Set(reflect.New(dst.Type().Elem()))
			}
			dst = dst.Elem()
		}
		if src.IsZero() {
			dst.SetZero()
			return nil
		}
		data, err := json.Marshal(src.Interface())
		if err != nil {
			return err
		}
		return json.Unmarshal(data, dst.Addr().Interface())
	}, nil
}
