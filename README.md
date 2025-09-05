## typeconv

Fast, reflection-planned conversions between Go types that share the same field names or JSON-like tags. Avoids JSON marshal/unmarshal for most struct-to-struct mappings while preserving correctness. Nested structs, slices, maps, and pointers are supported.

### Features

- Struct-to-struct conversion planned via reflection and cached
- Field mapping by tag (default `json`) or by field name (case-insensitive)
- Nested conversions: structs, `[]T`, and `map[string]T`
- Pointer semantics: auto-alloc dest pointers; nil source zeroes destination
- Per-call custom converters (no global registry)
- Optional strict typing
- Fallback to JSON round-trip when necessary

### Install

```bash
go get gitlab.com/quanata/projects/backend/go-util/typeconv
```

### Quick start

```go
package main

import (
    "fmt"
    tc "gitlab.com/quanata/projects/backend/go-util/typeconv"
)

type A struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

type B struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

func main() {
    a := A{ID: "123", Name: "hello"}
    var b B
    if err := tc.Convert(&b, &a); err != nil {
        panic(err)
    }
    fmt.Println(b)
}
```

### Per-call custom converters

Register converters per call using `Convert`’s variadic argument. Converters must be `func(dst *D, src *S) error`. They are applied to all nested occurrences of the matching types.

```go
type MyString string
type MyInt int

conv := func(dst *MyInt, src *MyString) error {
    // example: parse decimal string to int
    var n int
    _, err := fmt.Sscanf(string(*src), "%d", &n)
    if err != nil {
        return err
    }
    *dst = MyInt(n)
    return nil
}

type S struct { N MyString `json:"n"` }
type D struct { N MyInt    `json:"n"` }

s := S{N: MyString("42")}
var d D
if err := tc.Convert(&d, &s, conv); err != nil {
    panic(err)
}
```

Notes:
- Converters are matched by concrete (non-pointer) types `S -> D`.
- They propagate through nested conversions (structs, slices, maps).
- Duplicate converters for the same type pair in a single call will error.

### Options and planning

You can build and cache a plan with custom options. Plans convert quickly without re-planning, but do not accept per-call converters. Use top-level `Convert` when you need custom converters.

```go
// Options:
// - Tag: tag key to match fields (default "json")
// - StrictTypes: if true, disable reflect.Convert for trivially convertible types

type S struct { A int `db:"a"` }
type D struct { A int `db:"a"` }

p, err := tc.BuildPlan[S, D](tc.Options{Tag: "db", StrictTypes: false})
if err != nil { panic(err) }

var d D
s := S{A: 7}
if err := p.Convert(&d, &s); err != nil { panic(err) }
```

Strictness details:
- With `StrictTypes=false` (default), compatible primitives (e.g., `int` to `int64`) may use `reflect.Convert`.
- With `StrictTypes=true`, incompatible primitives avoid `reflect.Convert`; JSON fallback may still bridge types if the data marshals/unmarshals correctly.

### Supported conversions

- Structs: fields matched by tag key (default `json`); if tag is missing, match by field name (case-insensitive). Anonymous embedded structs are traversed when no explicit tag name is set.
- Slices `[]S` → `[]D`: element-wise conversion using the same rules
- Maps `map[string]S` → `map[string]D`: key must be string; values converted element-wise
- Pointers: destination pointers are auto-allocated; nil source results in zero value at destination
- Fallback: when no direct/registered/conversion path is available, a JSON round-trip is used for that leaf

### Error cases

- No overlapping fields found for the configured tag → error
- Destination field not settable (e.g., unexported) → error
- Duplicate per-call converters for the same type pair → error
- Custom converter returns non-nil error → bubbled up

### Behavioral notes

- Field matching is case-insensitive for tag values and untagged field names.
- JSON fallback treats zero-valued sources as zero, without attempting to marshal/unmarshal.
- Plans are cached by `(sourceType, destType, strict, tag)`. Per-call converters are not part of the cache key; they are only available via the top-level `Convert`.

### Benchmarks

Run:

```bash
go test -bench=. -benchmem
```

Sample results:

```text
goos: darwin
goarch: arm64
cpu: Apple M1 Pro

BenchmarkCompareTypeconvVsJSON/Typeconv-8    ~1369 ns/op    ~768 B/op     ~17 allocs/op
BenchmarkCompareTypeconvVsJSON/JSON-8        ~2631 ns/op   ~1217 B/op     ~27 allocs/op
BenchmarkCompareTypeconvVsJSON/Copier-8      ~6583 ns/op   ~5792 B/op     ~61 allocs/op
BenchmarkCompareTypeconvVsJSON/Mapstructure-8 ~6241 ns/op  ~5336 B/op     ~97 allocs/op
```

### Example matrix

```go
// Pointers
type SA struct { P *InnerA `json:"p"` }
type SB struct { P *InnerB `json:"p"` }
// - src nil  -> dst zero (nil)
// - src non-nil -> dst auto-allocated and converted

// Slices and maps
type XS struct { Items []ItemS `json:"items"`; M map[string]ItemS `json:"m"` }
type XD struct { Items []ItemD `json:"items"`; M map[string]ItemD `json:"m"` }

// Untagged name matching (case-insensitive)
type US struct { Name string; Count int }
type UD struct { NAME string; Count int }
```

### FAQ

- Why not a global registry? Per-call converters are explicit, safer in tests, and avoid global state in long-lived processes.
- Can I use a plan with custom converters? Not currently. Use the top-level `Convert` when you need per-call converters.
