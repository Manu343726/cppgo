package gen

import (
	"fmt"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// templateRequest is one -instantiate entry that names a concrete instantiation
// of a function or member function template.
//
// A class template is bound by naming a specialization the header declares; a
// function template is not, because there is no declaration to name: the
// compiler writes the function when it is called. The manifest entry carries
// the two facts the model cannot read — which type arguments to supply and
// which overload of the set — and the emitter writes them into the call:
//
//	-instantiate 'cv::Mat::at<unsigned char>(int, int)'
//
// becomes At on Mat, a call to `self->at<unsigned char>(i0, i1)`, and a Go
// method that returns the byte the reference names.
type templateRequest struct {
	// Entry is the manifest text, for diagnostics.
	Entry string
	// Record is the owning record's qualified name for a member template, empty
	// for a free function template.
	Record string
	// Member is the member function's own name, empty for a free function.
	Member string
	// Name is the qualified name of the template a free function declares.
	Name string
	// Args are the type arguments, in declaration order.
	Args []cxx.TemplateArgument
	// ArgSpellings are the same arguments as the manifest wrote them, for the
	// C++ call.
	ArgSpellings []string
	// Arity is the parameter count the entry named, -1 when it named none.
	Arity int
	// Const selects the const overload of a member set.
	Const bool
	// Symbol is the instantiated declaration the request resolved to.
	Symbol cxx.Symbol
}

// requestKey is the identity a resolved request shares with the binding built
// from it. The result spelling is part of it: at<unsigned char> and at<double>
// are two instantiations with one parameter list that differ only in what they
// return.
func (r *templateRequest) requestKey() string {
	return displaySignature(r.Symbol) + "|" + r.Symbol.Result.Spelling
}

// parseTemplateRequest parses one -instantiate entry written as
//
//	<qualified><T1, T2>(<param-type>, ...) [const]
//
// The parameter list is the overload selector: it is read for its count (and
// its constness marker), never for its types, because the pattern supplies the
// signature itself. The type arguments are the manifest's, and each one has to
// be a scalar the model can build.
func parseTemplateRequest(entry string) (*templateRequest, error) {
	text := strings.TrimSpace(entry)
	req := &templateRequest{Entry: entry, Arity: -1}
	if strings.HasSuffix(text, " const") {
		req.Const = true
		text = strings.TrimSpace(strings.TrimSuffix(text, " const"))
	}
	if strings.HasSuffix(text, ")") {
		open := matchingOpen(text, len(text)-1, '(', ')')
		if open < 0 {
			return nil, fmt.Errorf("cppgo: -instantiate %s: the parameter list is not balanced", entry)
		}
		list := strings.TrimSpace(text[open+1 : len(text)-1])
		text = strings.TrimSpace(text[:open])
		if list == "" {
			req.Arity = 0
		} else {
			req.Arity = len(splitTemplateArgs(list))
		}
	}
	open := strings.Index(text, "<")
	if open <= 0 || !strings.HasSuffix(text, ">") {
		return nil, fmt.Errorf("cppgo: -instantiate %s: a function instantiation is written <qualified><T1, T2>(<parameters>)", entry)
	}
	name := strings.TrimSpace(text[:open])
	args := splitTemplateArgs(text[open+1 : len(text)-1])
	if len(args) == 0 {
		return nil, fmt.Errorf("cppgo: -instantiate %s: the type argument list is empty", entry)
	}
	req.ArgSpellings = make([]string, len(args))
	req.Args = make([]cxx.TemplateArgument, len(args))
	for i, spelling := range args {
		spelling = strings.TrimSpace(spelling)
		param, ok := cxx.ScalarType(spelling)
		if !ok {
			return nil, fmt.Errorf("cppgo: -instantiate %s: the type argument %q is not a scalar the model can build; use a C++ scalar spelling such as 'unsigned char' or 'double'", entry, spelling)
		}
		req.ArgSpellings[i] = param.Spelling
		req.Args[i] = cxx.TemplateArgument{Kind: cxx.TemplateArgumentType, Spelling: param.Spelling, Type: param}
	}
	if i := strings.LastIndex(name, "::"); i > 0 {
		req.Record = name[:i]
		req.Member = name[i+2:]
	}
	req.Name = name
	return req, nil
}

// matchingOpen finds the opening bracket that matches the closing one at index
// close in text.
func matchingOpen(text string, close int, open, shut byte) int {
	depth := 0
	for i := close; i >= 0; i-- {
		switch text[i] {
		case shut:
			depth++
		case open:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// resolveTemplateRequests turns every -instantiate entry that names a function
// instantiation into a concrete declaration. A class specialization is handled
// by buildRecords, which matches it against the records libclang reported; a
// function or member instantiation has no record and is resolved here.
//
// The record a member instantiation belongs to must be part of the API: its Go
// value is the receiver. A member whose record the manifest does not bind is a
// manifest mistake, named with the record to add.
// resolveRecordRequests synthesizes the class specializations the manifest
// names. It runs before the records are classified, because a synthesized
// record is one of them: it needs a Go type name and a model like any other.
func (b *builder) resolveRecordRequests() error {
	for i, entry := range b.cfg.Instantiate {
		if b.instantiateMatched[i] || strings.Contains(entry, "(") {
			continue
		}
		templateName, args, ok := splitRecordInstantiation(entry)
		if !ok {
			continue
		}
		resolved := false
		for _, project := range b.projects {
			rec, ok := project.InstantiateRecord(templateName, args)
			if !ok {
				continue
			}
			seen := false
			for _, existing := range b.rawRecords {
				if existing.QualifiedName == rec.QualifiedName {
					seen = true
					break
				}
			}
			if !seen {
				b.rawRecords = append(b.rawRecords, rec)
			}
			if b.instantiated == nil {
				b.instantiated = map[string]bool{}
			}
			b.instantiated[rec.QualifiedName] = true
			b.instantiateMatched[i] = true
			resolved = true
			break
		}
		_ = resolved
	}
	return nil
}

// splitRecordInstantiation parses "cv::Scalar_<double>" into the pattern name
// and its arguments.
func splitRecordInstantiation(entry string) (string, []cxx.TemplateArgument, bool) {
	open := strings.Index(entry, "<")
	if open <= 0 || !strings.HasSuffix(entry, ">") {
		return "", nil, false
	}
	templateName := strings.TrimSpace(entry[:open])
	parts := splitTemplateArgs(entry[open+1 : len(entry)-1])
	args := make([]cxx.TemplateArgument, 0, len(parts))
	for _, part := range parts {
		arg, ok := cxx.ParseTemplateArgument(part)
		if !ok {
			return "", nil, false
		}
		args = append(args, arg)
	}
	return templateName, args, true
}

func (b *builder) resolveTemplateRequests() error {
	records := map[string]bool{}
	for _, rec := range b.rawRecords {
		records[rec.QualifiedName] = true
	}
	for i, entry := range b.cfg.Instantiate {
		if b.instantiateMatched[i] {
			continue
		}
		if !strings.Contains(entry, "(") {
			continue
		}
		req, err := parseTemplateRequest(entry)
		if err != nil {
			return err
		}
		resolved := false
		for _, project := range b.projects {
			var (
				sym cxx.Symbol
				ok  bool
			)
			if req.Record != "" && records[req.Record] {
				sym, ok = project.InstantiateMember(req.Record, req.Member, req.Args, req.Arity, req.Const)
			} else if req.Record == "" {
				sym, ok = project.InstantiateFunction(req.Name, req.Args, req.Arity)
			} else {
				return fmt.Errorf("cppgo: -instantiate %s: %s is not a record this header declares; bind the record in -bind and name the member here", entry, req.Record)
			}
			if ok {
				req.Symbol = sym
				resolved = true
				break
			}
		}
		if !resolved {
			return fmt.Errorf("cppgo: -instantiate %s: no declaration in the headers matches that instantiation; check the template's name, its type arguments and the parameter count", entry)
		}
		b.instantiateMatched[i] = true
		b.templateRequests = append(b.templateRequests, req)
		b.neededRecords[req.Record] = true
	}
	return nil
}

// requestFor finds the request a binding was built from.
func (b *builder) requestFor(sym cxx.Symbol) *templateRequest {
	for _, req := range b.templateRequests {
		if req.requestKey() == displaySignature(sym)+"|"+sym.Result.Spelling && req.Symbol.QualifiedName == sym.QualifiedName {
			return req
		}
	}
	return nil
}
