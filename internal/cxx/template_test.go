package cxx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSplitTemplateArgsKeepsNestedBrackets pins the splitter a specialization's
// request name goes through: a comma inside a nested angle bracket, a function
// type's parentheses or an array's brackets is not a separator.
func TestSplitTemplateArgsKeepsNestedBrackets(t *testing.T) {
	assert.Equal(t, []string{"int", "double"}, splitTemplateArgs("int, double"))
	assert.Equal(t,
		[]string{"std::vector<int>", "4"},
		splitTemplateArgs("std::vector<int>, 4"),
		"a nested angle bracket keeps its comma")
	assert.Equal(t,
		[]string{"int (*)(int, int)", "char"},
		splitTemplateArgs("int (*)(int, int), char"),
		"a function type's parentheses keep their commas")
	assert.Equal(t,
		[]string{"int[4]", "char"},
		splitTemplateArgs("int[4], char"),
		"an array's brackets keep their commas")
	assert.Empty(t, splitTemplateArgs(""), "no arguments, no parts")
	assert.Equal(t, []string{"int"}, splitTemplateArgs("int"))
}

// TestInnerTemplateArgumentsKeepsNestedBrackets covers the second splitter: the
// one that recovers a template or pack argument's name from the type spelling,
// where a nested angle bracket, a function type's parentheses or an array's
// brackets is not a separator.
func TestInnerTemplateArgumentsKeepsNestedBrackets(t *testing.T) {
	assert.Equal(t, []string{"std::vector<int>", "4"}, innerTemplateArguments("Q<std::vector<int>, 4>"))
	assert.Equal(t, []string{"int (*)(int)"}, innerTemplateArguments("Q<int (*)(int)>"))
	assert.Equal(t, []string{"int[4]"}, innerTemplateArguments("Q<int[4]>"))
	assert.Nil(t, innerTemplateArguments("Q"), "no brackets, no arguments")
	assert.Nil(t, innerTemplateArguments("Q<int"), "an unbalanced spelling yields nothing")
}
