package model_test

import (
	"testing"

	"maragu.dev/is"

	"app/model"
)

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "lowercases a shouty query", input: "CATS", expected: "cats"},
		{name: "trims surrounding whitespace", input: "  cats  ", expected: "cats"},
		{name: "collapses internal whitespace", input: "why  do   cats\tpurr", expected: "why do cats purr"},
		{name: "normalises newlines as single spaces", input: "cats\nand\ndogs", expected: "cats and dogs"},
		{name: "leaves an empty string alone", input: "", expected: ""},
		{name: "treats a whitespace-only string as empty", input: "   \t\n ", expected: ""},
		{name: "preserves unicode in the query", input: "CAFÉ ", expected: "café"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			is.Equal(t, test.expected, model.NormalizeQuery(test.input))
		})
	}
}
