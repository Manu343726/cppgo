package cxx

import "strings"

// ScalarType maps a C++ scalar spelling onto the parameter the model carries
// for it: the kind, libclang's own kind name, the width and the signedness.
//
// It exists for a template argument a manifest names in text. A template
// instantiation is not a declaration libclang visits, so the generator has to
// build the argument itself, and the substitute-into-the-pattern step needs the
// same facts a parsed parameter carries. The spellings are the ones a reader
// writes in a manifest: the C++ keywords, the OpenCV aliases for them
// (`uchar`), and the fixed-width names.
func ScalarType(spelling string) (Param, bool) {
	s := strings.TrimSpace(spelling)
	s = strings.TrimPrefix(s, "const ")
	s = strings.TrimSpace(s)
	if scalar, ok := scalarTypes[s]; ok {
		return Param{
			Kind:      scalar.kind,
			ClangKind: scalar.clangKind,
			Spelling:  s,
			SizeBytes: scalar.size,
			Unsigned:  scalar.unsigned,
		}, true
	}
	return Param{}, false
}

// scalarType is one row of the table.
type scalarType struct {
	kind      TypeKind
	clangKind string
	size      int64
	unsigned  bool
}

// scalarTypes is the table ScalarType reads. The alias spellings are the ones a
// library installs for its own API (OpenCV's uchar and schar, the fixed-width
// integer names), so a manifest can name the type the header's own declaration
// names.
var scalarTypes = map[string]scalarType{
	"bool":               {TypeInteger, "Bool", 1, false},
	"char":               {TypeInteger, "CharS", 1, false},
	"signed char":        {TypeInteger, "SChar", 1, false},
	"unsigned char":      {TypeInteger, "UChar", 1, true},
	"uchar":              {TypeInteger, "UChar", 1, true},
	"schar":              {TypeInteger, "SChar", 1, false},
	"short":              {TypeInteger, "Short", 2, false},
	"short int":          {TypeInteger, "Short", 2, false},
	"unsigned short":     {TypeInteger, "UShort", 2, true},
	"unsigned short int": {TypeInteger, "UShort", 2, true},
	"int":                {TypeInteger, "Int", 4, false},
	"unsigned int":       {TypeInteger, "UInt", 4, true},
	"unsigned":           {TypeInteger, "UInt", 4, true},
	"long":               {TypeInteger, "Long", 8, false},
	"long int":           {TypeInteger, "Long", 8, false},
	"unsigned long":      {TypeInteger, "ULong", 8, true},
	"long long":          {TypeInteger, "LongLong", 8, false},
	"unsigned long long": {TypeInteger, "ULongLong", 8, true},
	"float":              {TypeFloat, "Float", 4, false},
	"double":             {TypeFloat, "Double", 8, false},
	"size_t":             {TypeInteger, "ULong", 8, true},
	"std::size_t":        {TypeInteger, "ULong", 8, true},
	"std::int8_t":        {TypeInteger, "SChar", 1, false},
	"int8_t":             {TypeInteger, "SChar", 1, false},
	"std::uint8_t":       {TypeInteger, "UChar", 1, true},
	"uint8_t":            {TypeInteger, "UChar", 1, true},
	"std::int16_t":       {TypeInteger, "Short", 2, false},
	"int16_t":            {TypeInteger, "Short", 2, false},
	"std::uint16_t":      {TypeInteger, "UShort", 2, true},
	"uint16_t":           {TypeInteger, "UShort", 2, true},
	"std::int32_t":       {TypeInteger, "Int", 4, false},
	"int32_t":            {TypeInteger, "Int", 4, false},
	"std::uint32_t":      {TypeInteger, "UInt", 4, true},
	"uint32_t":           {TypeInteger, "UInt", 4, true},
	"std::int64_t":       {TypeInteger, "Long", 8, false},
	"int64_t":            {TypeInteger, "Long", 8, false},
	"std::uint64_t":      {TypeInteger, "ULong", 8, true},
	"uint64_t":           {TypeInteger, "ULong", 8, true},
	"std::ptrdiff_t":     {TypeInteger, "Long", 8, false},
	"ptrdiff_t":          {TypeInteger, "Long", 8, false},
}
