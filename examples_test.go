package typeconv_test

import (
	"fmt"

	tc "github.com/vaughanb/typeconv"
)

type exampleSrc struct {
	Name string `json:"name"`
}

type exampleDst struct {
	Name string `json:"name"`
}

func ExampleConvert() {
	s := exampleSrc{Name: "Alice"}
	var d exampleDst

	_ = tc.Convert(&d, &s)
	fmt.Println(d.Name)
	// Output: Alice
}
