// Package errormap turns a C++ failure convention into a Go error.
//
// A C++ library reports failure in at least eight ways: it throws, it returns a
// std::error_code, a std::expected, a status int, an errno, a bool with an out
// parameter, a project-specific Result<T>, or a std::variant whose second
// alternative is the error. The contract says every one of them becomes the
// same thing on the Go side — an error as the last result — and this package is
// the part of that mapping the generated code shares: the projection of a C++
// failure into a *ErrorCode or a *Message, and the per-package configuration
// for the conventions that cannot be inferred from the declaration.
//
// What is inferred and what is configured:
//
//   - exception, std::error_code and a result type whose shape is detectable
//     (has_value/value/error) need no entry: the declaration says what it is;
//   - a status int, an errno, a bool-with-out-parameters or a std::variant
//     cannot be told from a signature that returns int, and a config entry
//     names the channels;
//   - an entry is validated against the declaration at generation time — a
//     parameter that does not exist, or one that cannot carry the channel, is a
//     generation error and no output is written. A config cannot invent a
//     signature.
package errormap

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Kind is a C++ failure convention.
type Kind string

const (
	// KindException means the call can throw: the cppgo-built shim catches
	// std::exception (and ...) and the Go call returns an error.
	KindException Kind = "exception"
	// KindErrorCode means the call returns std::error_code: zero is success,
	// non-zero carries a code, a category and a message.
	KindErrorCode Kind = "error_code"
	// KindExpected means the call returns std::expected<T,E> or tl::expected.
	KindExpected Kind = "expected"
	// KindOptional means the call returns std::optional<T>. By default an
	// empty optional is not a failure and the Go call is (T, bool); a config
	// entry with an absent message turns it into (T, error).
	KindOptional Kind = "optional"
	// KindStatus means the call returns a status int: zero is success.
	KindStatus Kind = "status"
	// KindErrno means the call reports failure through errno.
	KindErrno Kind = "errno"
	// KindOutParam means the call returns bool (or a status) that is not
	// itself the error, with the value and the failure in out parameters.
	KindOutParam Kind = "out_param"
	// KindCustom means the call returns a project type whose accessors the
	// config names.
	KindCustom Kind = "custom"
	// KindVariant means the call returns std::variant<T,E> with a known error
	// alternative: a config rule, never a guess.
	KindVariant Kind = "variant"
)

// Param is one parameter of a declaration, as the generator knows it from the
// header.
type Param struct {
	// Name is the parameter's name, when the declaration has one.
	Name string
	// Pointer reports whether the parameter is a pointer or a reference: the
	// only shape that can carry an out-value or an out-error.
	Pointer bool
}

// Signature is what the generator knows about a declaration.
type Signature struct {
	// Name is the qualified C++ name: "ns::parse".
	Name string
	// Returns is the return type's spelling, for the report and the error
	// messages.
	Returns string
	// Params are the parameters in declaration order.
	Params []Param
}

// Shape is the member surface of a return type, as libclang reports it. It is
// how a Result-like type is inferred without configuration.
type Shape struct {
	// HasValue reports a has_value() (or equivalent) member.
	HasValue bool
	// Value reports a value() member.
	Value bool
	// Error reports an error() member.
	Error bool
}

// Infer returns the kind a return type's shape implies, and whether the shape
// was recognised. The three members together are the shape tl::expected,
// std::expected and most project Result<T> types share; anything else is
// either configured or not an error type at all.
func Infer(s Shape) (Kind, bool) {
	if s.HasValue && s.Value && s.Error {
		return KindCustom, true
	}
	return "", false
}

// Entry is one symbol's configuration: the conventions a signature cannot
// state.
type Entry struct {
	// Kind is the failure convention.
	Kind Kind `yaml:"kind"`
	// Value names the channel that carries the result: "return" or a
	// parameter index.
	Value string `yaml:"value,omitempty"`
	// Error names the channel that carries the failure: "return" or a
	// parameter index.
	Error string `yaml:"error,omitempty"`
	// Type is the optional projection: the C++ type of the error, when the
	// generated Go error type is derived from it.
	Type string `yaml:"type,omitempty"`
	// Absent is the message an empty std::optional becomes. Empty (the
	// default) means absence is not a failure and the Go call is (T, bool).
	Absent string `yaml:"absent,omitempty"`
	// Alternative is the std::variant alternative that carries the error.
	// It is required for kind "variant" and meaningless for every other kind.
	Alternative *int `yaml:"alternative,omitempty"`
	// HasValue, ValueAccessor and ErrorAccessor name the members of a custom
	// result type the generator could not infer.
	HasValue      string `yaml:"has_value,omitempty"`
	ValueAccessor string `yaml:"value_accessor,omitempty"`
	ErrorAccessor string `yaml:"error_accessor,omitempty"`
}

// Config is a package's error configuration: the file a package writes when a
// convention cannot be inferred.
type Config struct {
	// Errors maps a qualified C++ declaration to its entry.
	Errors map[string]Entry `yaml:"errors"`
}

// Load parses a configuration file.
func Load(data []byte) (*Config, error) {
	var c Config

	// Unknown keys are rejected rather than ignored: a mistyped channel
	// ("valuee:", "err:") would otherwise leave the entry with no channels at
	// all, and an entry that names nothing is exactly what this file exists to
	// prevent.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("errormap: parsing config: %w", err)
	}
	for name, entry := range c.Errors {
		if entry.Kind == "" {
			return nil, fmt.Errorf("errormap: %s: no kind", name)
		}
		if !entry.Kind.valid() {
			return nil, fmt.Errorf("errormap: %s: unknown kind %q", name, entry.Kind)
		}
	}
	return &c, nil
}

func (k Kind) valid() bool {
	switch k {
	case KindException, KindErrorCode, KindExpected, KindOptional, KindStatus,
		KindErrno, KindOutParam, KindCustom, KindVariant:
		return true
	}
	return false
}

// Rule is one symbol's validated mapping.
type Rule struct {
	// Symbol is the qualified C++ declaration.
	Symbol string
	// Kind is the failure convention.
	Kind Kind
	// Value is the channel that carries the result: -1 for the return value,
	// otherwise the parameter index.
	Value int
	// Error is the channel that carries the failure: -1 for the return value,
	// otherwise the parameter index.
	Error int
	// Projection is the C++ type of the error, when one was named.
	Projection string
	// Absent is the message an empty std::optional becomes; empty means the
	// generated call is (T, bool).
	Absent string
	// Alternative is the std::variant alternative that carries the error, or
	// -1 when the rule is not a variant.
	Alternative int
	// Configured reports whether the rule came from a config entry or from
	// the declaration's own shape.
	Configured bool
}

// Resolve validates the configuration for sig and returns the mapping to emit.
//
// The rules the validation enforces, and why each one exists:
//
//   - an entry whose symbol is not a declaration the generator knows is an
//     error: a config cannot invent a signature;
//   - a channel must be "return" or an existing parameter index, named by
//     index or by the parameter's name;
//   - an out-parameter channel must be a pointer or a reference: a value
//     parameter cannot carry a result back;
//   - a kind that is inferred from the declaration needs no channel, and an
//     entry for one may only add a projection.
//
// infer carries what the declaration alone says: the kind (exception, error
// code, expected, a Result-shaped type) and whether it is fallible at all.
func (c *Config) Resolve(sig Signature, infer Inference) (Rule, error) {
	rule := Rule{Symbol: sig.Name, Value: -1, Error: -1, Alternative: -1, Kind: infer.Kind}

	entry, configured := Entry{}, false
	if c != nil {
		entry, configured = c.Errors[sig.Name]
	}

	if !configured {
		switch {
		case !infer.Fallible && infer.Kind == "":
			// A call that cannot fail gets no error result at all.
			return Rule{Symbol: sig.Name, Value: -1, Error: -1, Alternative: -1}, nil
		case infer.Kind == "":
			// The call can fail and the declaration does not say how. Silence
			// here would be a failure dropped on the floor, which is the one
			// thing this mapping may not do.
			return Rule{}, fmt.Errorf(
				"errormap: %s: the declaration can fail but states no error convention; add a config entry naming one (kind: out_param|status|errno|custom|variant, with its value and error channels)",
				sig.Name)
		default:
			rule.Kind = infer.Kind
			return rule, nil
		}
	}

	rule.Configured = true
	rule.Kind = entry.Kind
	rule.Projection = entry.Type

	// A kind whose error is part of what it returns may not also name a
	// channel: there would be two places to read the failure from, and the
	// generated code would have to guess which one the library means.
	switch entry.Kind {
	case KindException:
		return Rule{}, fmt.Errorf("errormap: %s: exceptions are inferred from the declaration and cannot be configured", sig.Name)
	case KindErrorCode, KindExpected:
		if entry.Value != "" || entry.Error != "" {
			return Rule{}, fmt.Errorf(
				"errormap: %s: kind %q returns its value and its error, so it takes no channels",
				sig.Name, entry.Kind)
		}
	case KindStatus, KindErrno:
		if entry.Error != "" && entry.Error != "return" {
			return Rule{}, fmt.Errorf(
				"errormap: %s: kind %q reports through the return value, not through %q",
				sig.Name, entry.Kind, entry.Error)
		}
	case KindOptional:
		if entry.Value != "" || entry.Error != "" {
			return Rule{}, fmt.Errorf(
				"errormap: %s: kind %q returns its value, or reports its absence through the return, so it takes no channels",
				sig.Name, entry.Kind)
		}
	case KindVariant:
		if entry.Alternative == nil {
			return Rule{}, fmt.Errorf(
				"errormap: %s: kind %q must name the error alternative (alternative: <index>)", sig.Name, entry.Kind)
		}
		if entry.Value != "" || entry.Error != "" {
			return Rule{}, fmt.Errorf(
				"errormap: %s: kind %q carries its value and its error in alternatives, so it takes no channels",
				sig.Name, entry.Kind)
		}
	case KindOutParam:
		if entry.Value == "" || entry.Error == "" {
			return Rule{}, fmt.Errorf(
				"errormap: %s: kind %q must name both a value channel and an error channel", sig.Name, entry.Kind)
		}
	}

	// The two kind-specific fields are meaningless elsewhere, and a field
	// that does nothing is the defect this validation exists to prevent.
	if entry.Absent != "" && entry.Kind != KindOptional {
		return Rule{}, fmt.Errorf(
			"errormap: %s: absent is only meaningful for kind %q, not %q", sig.Name, KindOptional, entry.Kind)
	}
	if entry.Alternative != nil && entry.Kind != KindVariant {
		return Rule{}, fmt.Errorf(
			"errormap: %s: alternative is only meaningful for kind %q, not %q", sig.Name, KindVariant, entry.Kind)
	}
	if entry.Absent != "" {
		rule.Absent = entry.Absent
	}
	if entry.Alternative != nil {
		rule.Alternative = *entry.Alternative
	}

	// The value channel: where the result is written, when it is not the
	// return value.
	if entry.Value != "" && entry.Value != "return" {
		index, err := sig.channel(entry.Value)
		if err != nil {
			return Rule{}, fmt.Errorf("errormap: %s: value channel: %w", sig.Name, err)
		}
		if !sig.Params[index].Pointer {
			return Rule{}, fmt.Errorf(
				"errormap: %s: value channel %q is parameter %d, which is not a pointer or reference and cannot carry a result back",
				sig.Name, entry.Value, index)
		}
		rule.Value = index
	}

	// The error channel: where the failure is reported, when it is not the
	// return value.
	if entry.Error != "" && entry.Error != "return" {
		index, err := sig.channel(entry.Error)
		if err != nil {
			return Rule{}, fmt.Errorf("errormap: %s: error channel: %w", sig.Name, err)
		}
		if !sig.Params[index].Pointer {
			return Rule{}, fmt.Errorf(
				"errormap: %s: error channel %q is parameter %d, which is not a pointer or reference and cannot report a failure",
				sig.Name, entry.Error, index)
		}
		rule.Error = index
	}

	return rule, nil
}

// Inference is what a declaration says about failure on its own.
type Inference struct {
	// Kind is the convention the declaration implies.
	Kind Kind
	// Fallible reports whether the call can fail at all. A noexcept function
	// returning a plain int is not fallible and gets no error result.
	Fallible bool
}

// channel resolves "return" or a parameter index/name to -1 or an index.
func (s Signature) channel(name string) (int, error) {
	if index, err := strconv.Atoi(name); err == nil {
		if index < 0 || index >= len(s.Params) {
			return 0, fmt.Errorf("parameter %d does not exist; the declaration takes %s", index, s.paramList())
		}
		return index, nil
	}
	for i, p := range s.Params {
		if p.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("parameter %q does not exist; the declaration takes %s", name, s.paramList())
}

// paramList spells the parameters for an error message, so a wrong channel is
// actionable without opening the header.
func (s Signature) paramList() string {
	if len(s.Params) == 0 {
		return "no parameters"
	}
	parts := make([]string, len(s.Params))
	for i, p := range s.Params {
		name := p.Name
		if name == "" {
			name = "?"
		}
		parts[i] = fmt.Sprintf("%d:%s", i, name)
	}
	return strings.Join(parts, ", ")
}

// Report renders the per-symbol line the generator writes into its report: what
// the mechanism is and whether it came from the declaration or from the config.
func (r Rule) Report() string {
	source := "inferred"
	if r.Configured {
		source = "configured"
	}
	if r.Kind == "" {
		return fmt.Sprintf("%s\tno error channel\t%s", r.Symbol, source)
	}
	return fmt.Sprintf("%s\t%s\t%s\tvalue=%s error=%s",
		r.Symbol, r.Kind, source, r.channel(r.Value), r.channel(r.Error))
}

func (r Rule) channel(index int) string {
	if index < 0 {
		return "return"
	}
	return strconv.Itoa(index)
}

// Symbols returns the configured symbols, sorted.
func (c *Config) Symbols() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.Errors))
	for name := range c.Errors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate checks every configured symbol against the declarations the package
// binds, and returns the first problem it finds.
//
// This is the rule that keeps a config honest in the other direction from
// Resolve: Resolve says a declaration's entry names existing channels, Validate
// says an entry names a declaration that exists at all. A typo in a symbol name
// would otherwise sit in the config doing nothing, exactly like a `-rename`
// that matches no binding.
func (c *Config) Validate(sigs []Signature) error {
	known := make(map[string]bool, len(sigs))
	for _, sig := range sigs {
		known[sig.Name] = true
	}

	for _, name := range c.Symbols() {
		if !known[name] {
			return fmt.Errorf(
				"errormap: config names %q, which is not a declaration this package binds (%s)",
				name, strings.Join(sortedKeys(known), ", "))
		}
	}
	return nil
}

// Apply validates the config against sigs and resolves every declaration's
// rule. It is the generator's single entry point: the rules are what the
// emitter turns into `(T, error)` signatures, and an error here means no output
// is written.
func (c *Config) Apply(sigs []Signature, infer func(Signature) Inference) ([]Rule, error) {
	if err := c.Validate(sigs); err != nil {
		return nil, err
	}

	rules := make([]Rule, 0, len(sigs))
	for _, sig := range sigs {
		rule, err := c.Resolve(sig, infer(sig))
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
