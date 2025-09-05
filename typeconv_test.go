package typeconv

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type CustomTypeA string

func (c CustomTypeA) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(c))
}

type CustomTypeB int

func (c *CustomTypeB) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*c = CustomTypeB(v)
	return nil
}

// Add untagged and custom fields to A and B
type A struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Meta     map[string]int `json:"meta"`
	Items    []ItemA        `json:"items"`
	Untagged string         // no tag
	Custom   CustomTypeA    `json:"custom"`
}

type ItemA struct {
	Value int `json:"value"`
}

type B struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Meta     map[string]int `json:"meta"`
	Items    []ItemB        `json:"items"`
	Untagged string         // no tag
	Custom   CustomTypeB    `json:"custom"`
}

type ItemB struct {
	Value int `json:"value"`
}

type InternalDate struct{ Y, M, D int }

func (d InternalDate) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("%04d-%02d-%02d", d.Y, d.M, d.D))
}

type ExternalDate struct{ Raw string }

func (d *ExternalDate) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &d.Raw)
}

func TestConvertBasic(t *testing.T) {
	a := A{ID: "123", Name: "hello", Meta: map[string]int{"x": 1}, Items: []ItemA{{Value: 5}}}
	var b B
	require.NoError(t, Convert(&b, &a), "Convert failed")
	assert.Equal(t, "123", b.ID)
	assert.Equal(t, "hello", b.Name)
	assert.Len(t, b.Items, 1)
	assert.Equal(t, 5, b.Items[0].Value)
}

func TestJSONFallback(t *testing.T) {
	type Src struct {
		Date InternalDate `json:"date"`
	}
	type Dst struct {
		Date ExternalDate `json:"date"`
	}

	s := Src{Date: InternalDate{2025, 9, 3}}
	var d Dst

	require.NoError(t, Convert(&d, &s), "fallback failed")
	assert.NotEmpty(t, d.Date.Raw, "expected non-empty date")
}

func TestCustomConverter(t *testing.T) {
	type MyString string
	type MyInt int

	// Provide a custom converter from MyString to MyInt per call
	conv := func(dst *MyInt, src *MyString) error {
		v, err := strconv.Atoi(string(*src))
		if err != nil {
			return err
		}
		*dst = MyInt(v)
		return nil
	}

	type Src struct {
		N MyString `json:"n"`
	}
	type Dst struct {
		N MyInt `json:"n"`
	}

	s := Src{N: MyString("42")}
	var d Dst
	require.NoError(t, Convert(&d, &s, conv), "Convert failed")
	assert.Equal(t, 42, int(d.N))
}

func TestStrictTypesConvertibleStillWorksViaJSON(t *testing.T) {
	type S struct {
		N int `json:"n"`
	}
	type D struct {
		N int64 `json:"n"`
	}

	p, err := BuildPlan[S, D](Options{Tag: "json", StrictTypes: true})
	require.NoError(t, err, "BuildPlan failed")
	s := S{N: 7}
	var d D
	require.NoError(t, p.Convert(&d, &s), "Convert failed")
	assert.Equal(t, int64(7), d.N)
}

func TestCustomTagSupport(t *testing.T) {
	type S struct {
		A int `db:"a"`
	}
	type D struct {
		A int `db:"a"`
	}

	p, err := BuildPlan[S, D](Options{Tag: "db"})
	require.NoError(t, err, "BuildPlan failed")
	s := S{A: 13}
	var d D
	require.NoError(t, p.Convert(&d, &s), "Convert failed")
	assert.Equal(t, 13, d.A)
}

func TestCaseInsensitiveTags(t *testing.T) {
	type S struct {
		Name string `json:"Name"`
	}
	type D struct {
		Name string `json:"name"`
	}

	s := S{Name: "X"}
	var d D
	require.NoError(t, Convert(&d, &s), "Convert failed")
	assert.Equal(t, "X", d.Name)
}

func TestPointerHandling(t *testing.T) {
	// Nil source pointer zeroes destination
	{
		type InnerA struct {
			V int `json:"v"`
		}
		type InnerB struct {
			V int `json:"v"`
		}
		type S struct {
			P *InnerA `json:"p"`
		}
		type D struct {
			P *InnerB `json:"p"`
		}

		s := S{P: nil}
		var d D
		require.NoError(t, Convert(&d, &s))
		assert.Nil(t, d.P)
	}

	// Destination pointer auto-allocated
	{
		type InnerA struct {
			V int `json:"v"`
		}
		type InnerB struct {
			V int `json:"v"`
		}
		type S struct {
			P *InnerA `json:"p"`
		}
		type D struct {
			P *InnerB `json:"p"`
		}

		s := S{P: &InnerA{V: 21}}
		var d D
		require.NoError(t, Convert(&d, &s), "Convert failed")
		if assert.NotNil(t, d.P) {
			assert.Equal(t, 21, d.P.V)
		}
	}
}

func TestSliceConversionPointers(t *testing.T) {
	type IA struct {
		V int `json:"v"`
	}
	type IB struct {
		V int `json:"v"`
	}
	type S struct {
		Items []*IA `json:"items"`
	}
	type D struct {
		Items []IB `json:"items"`
	}

	s := S{Items: []*IA{{V: 1}, {V: 2}}}
	var d D
	require.NoError(t, Convert(&d, &s), "Convert failed")
	assert.Len(t, d.Items, 2)
	assert.Equal(t, 1, d.Items[0].V)
	assert.Equal(t, 2, d.Items[1].V)
}

func TestMapConversionNested(t *testing.T) {
	type IA struct {
		V int `json:"v"`
	}
	type IB struct {
		V int `json:"v"`
	}
	type S struct {
		M map[string]IA `json:"m"`
	}
	type D struct {
		M map[string]IB `json:"m"`
	}

	// Non-nil map
	s := S{M: map[string]IA{"a": {V: 7}}}
	var d D
	require.NoError(t, Convert(&d, &s), "Convert failed")
	assert.Len(t, d.M, 1)
	assert.Equal(t, 7, d.M["a"].V)

	// Nil map -> zero value at destination
	s2 := S{M: nil}
	var d2 D
	require.NoError(t, Convert(&d2, &s2), "Convert failed")
	assert.Nil(t, d2.M)
}

func TestJSONFallbackNestedStruct(t *testing.T) {
	// Use nested wrappers so the fallback occurs below the top-level struct
	type SA struct {
		Date InternalDate `json:"date"`
	}
	type SB struct {
		Date ExternalDate `json:"date"`
	}
	type S struct {
		X SA `json:"x"`
	}
	type D struct {
		X SB `json:"x"`
	}

	s := S{X: SA{Date: InternalDate{2024, 12, 31}}}
	var d D
	require.NoError(t, Convert(&d, &s), "Convert failed")
	assert.NotEmpty(t, d.X.Date.Raw)
}

func TestCustomConverterPrecedence(t *testing.T) {
	type Alpha struct {
		V int `json:"v"`
	}
	type Beta struct {
		V int `json:"v"`
	}

	conv := func(dst *Beta, src *Alpha) error {
		dst.V = 99
		return nil
	}

	type S struct {
		X Alpha `json:"x"`
	}
	type D struct {
		X Beta `json:"x"`
	}

	s := S{X: Alpha{V: 1}}
	var d D
	require.NoError(t, Convert(&d, &s, conv), "Convert failed")
	assert.Equal(t, 99, d.X.V)
}

func TestPlanCaching(t *testing.T) {
	type S struct {
		A int `json:"a"`
	}
	type D struct {
		A int `json:"a"`
	}

	p1, err := BuildPlan[S, D](Options{Tag: "json"})
	require.NoError(t, err, "BuildPlan 1 failed")
	p2, err := BuildPlan[S, D](Options{Tag: "json"})
	require.NoError(t, err, "BuildPlan 2 failed")
	assert.Equal(t, p1, p2, "expected cached plan pointer equality")
}

func TestUntaggedFieldNameMatching(t *testing.T) {
	type S struct {
		Name  string
		Count int
	}
	type D struct {
		NAME  string // different casing to ensure case-insensitive match
		Count int
	}

	s := S{Name: "foo", Count: 42}
	var d D
	require.NoError(t, Convert(&d, &s), "Convert failed")
	assert.Equal(t, "foo", d.NAME)
	assert.Equal(t, 42, d.Count)
}

// Benchmark comparing typeconv vs JSON vs other libraries
// for converting between two structs with similar fields.
//
// Requires:
//   go get github.com/jinzhu/copier
//   go get github.com/go-viper/mapstructure/v2
//
//func BenchmarkCompareTypeconvVsJSON(b *testing.B) {
//	// Per-call custom converter for CustomTypeA -> CustomTypeB
//	cconv := func(dst *CustomTypeB, src *CustomTypeA) error {
//		v, err := strconv.Atoi(string(*src))
//		if err != nil {
//			return err
//		}
//		*dst = CustomTypeB(v)
//		return nil
//	}
//
//	a := A{
//		ID:       "bench",
//		Name:     "world",
//		Meta:     map[string]int{"x": 42},
//		Items:    []ItemA{{Value: 5}, {Value: 6}},
//		Untagged: "untagged-value",
//		Custom:   CustomTypeA("1234"),
//	}
//	var bb B
//
//	b.Run("Typeconv", func(b *testing.B) {
//		b.ReportAllocs()
//		for i := 0; i < b.N; i++ {
//			if err := Convert(&bb, &a, cconv); err != nil {
//				b.Fatal(err)
//			}
//		}
//	})
//
//	b.Run("JSON", func(b *testing.B) {
//		b.ReportAllocs()
//		for i := 0; i < b.N; i++ {
//			data, _ := json.Marshal(a)
//			var out B
//			if err := json.Unmarshal(data, &out); err != nil {
//				b.Fatal(err)
//			}
//		}
//	})
//
//	// Benchmark using github.com/jinzhu/copier
//	b.Run("Copier", func(b *testing.B) {
//		b.ReportAllocs()
//		opt := copier.Option{
//			DeepCopy: true,
//			Converters: []copier.TypeConverter{
//				{
//					SrcType: reflect.TypeOf(CustomTypeA("")),
//					DstType: reflect.TypeOf(CustomTypeB(0)),
//					Fn: func(src interface{}) (interface{}, error) {
//						s := src.(CustomTypeA)
//						v, err := strconv.Atoi(string(s))
//						if err != nil {
//							return nil, err
//						}
//						return CustomTypeB(v), nil
//					},
//				},
//			},
//		}
//		for i := 0; i < b.N; i++ {
//			var out B
//			if err := copier.CopyWithOption(&out, &a, opt); err != nil {
//				b.Fatal(err)
//			}
//		}
//	})
//
//	// Benchmark using github.com/go-viper/mapstructure/v2
//	b.Run("Mapstructure", func(b *testing.B) {
//		b.ReportAllocs()
//		for i := 0; i < b.N; i++ {
//			var out B
//			cfg := &mapstructure.DecoderConfig{
//				TagName: "json",
//				Result:  &out,
//				DecodeHook: func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
//					if from == reflect.TypeOf(CustomTypeA("")) && to == reflect.TypeOf(CustomTypeB(0)) {
//						s := data.(CustomTypeA)
//						v, err := strconv.Atoi(string(s))
//						if err != nil {
//							return nil, err
//						}
//						return CustomTypeB(v), nil
//					}
//					return data, nil
//				},
//			}
//			dec, err := mapstructure.NewDecoder(cfg)
//			if err != nil {
//				b.Fatal(err)
//			}
//			if err := dec.Decode(a); err != nil {
//				b.Fatal(err)
//			}
//		}
//	})
//}
