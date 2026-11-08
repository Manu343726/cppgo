package gen

import (
	"strings"
)

// docCommands are the Doxygen and javadoc-style commands a C++ doc comment
// starts a line with. The marker is dropped and the rest of the line is kept,
// so the prose a reader wrote survives and only the markup goes. A command with
// no text of its own is dropped entirely.
var docCommands = map[string]string{
	"brief":      "",
	"details":    "",
	"file":       "",
	"class":      "",
	"struct":     "",
	"enum":       "",
	"param":      "",
	"tparam":     "",
	"arg":        "",
	"returns":    "Returns ",
	"return":     "Returns ",
	"result":     "Returns ",
	"retval":     "Returns ",
	"note":       "Note: ",
	"warning":    "Warning: ",
	"see":        "See ",
	"throws":     "Throws ",
	"throw":      "Throws ",
	"deprecated": "Deprecated: ",
}

// paragraphCommands are the commands that introduce a section of their own, so
// the line they start begins a new Go paragraph instead of continuing the
// previous sentence.
var paragraphCommands = map[string]bool{
	"details": true, "returns": true, "return": true, "result": true,
	"retval": true, "note": true, "warning": true, "see": true,
	"throws": true, "throw": true, "deprecated": true,
}

// docComment turns a raw C++ comment into Go doc text: the comment markers are
// stripped, the Doxygen command markers are replaced by the word a Go reader
// expects, blank lines are collapsed, and the result is trimmed. Paragraphs are
// separated by one blank line, which is what Go's doc renderer reads as a new
// paragraph.
func docComment(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var paragraphs [][]string
	var current []string
	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, current)
			current = nil
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		command, hasCommand := docCommand(line)
		if hasCommand && paragraphCommands[command] {
			flush()
		}
		text := stripDocLine(line)
		if text == "" {
			flush()
			continue
		}
		current = append(current, text)
	}
	flush()
	out := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		out = append(out, strings.Join(paragraph, "\n"))
	}
	return strings.Join(out, "\n\n")
}

// docCommand reports the command a comment line starts with, if any.
func docCommand(line string) (string, bool) {
	text := strings.TrimSpace(line)
	for _, prefix := range []string{"/**", "/*!", "/*", "///", "//!", "//", "*"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}
	word, _, ok := splitCommand(text)
	return word, ok
}

// stripDocLine strips one comment line's markers and command and returns its text.
func stripDocLine(line string) string {
	text := strings.TrimRight(line, " \t\r")
	text = strings.TrimSpace(text)
	// The closing marker may end the block's first line or stand on its own.
	if strings.HasSuffix(text, "*/") && !strings.HasPrefix(text, "*/") {
		text = strings.TrimSpace(strings.TrimSuffix(text, "*/"))
	} else if text == "*/" {
		return ""
	}
	switch {
	case strings.HasPrefix(text, "/**"):
		text = strings.TrimPrefix(text, "/**")
	case strings.HasPrefix(text, "/*!"):
		text = strings.TrimPrefix(text, "/*!")
	case strings.HasPrefix(text, "/*"):
		text = strings.TrimPrefix(text, "/*")
	case strings.HasPrefix(text, "///"):
		text = strings.TrimPrefix(text, "///")
	case strings.HasPrefix(text, "//!"):
		text = strings.TrimPrefix(text, "//!")
	case strings.HasPrefix(text, "//"):
		text = strings.TrimPrefix(text, "//")
	}
	text = strings.TrimSpace(text)
	// A leading "*" is the continuation marker of a block comment.
	if text == "*" {
		return ""
	}
	if strings.HasPrefix(text, "* ") || strings.HasPrefix(text, "*\t") {
		text = strings.TrimSpace(text[1:])
	} else if strings.HasPrefix(text, "*") && !strings.HasPrefix(text, "*/") {
		text = strings.TrimSpace(text[1:])
	}
	if text == "" {
		return ""
	}
	if word, rest, ok := splitCommand(text); ok {
		replacement, known := docCommands[word]
		if !known {
			// An unknown command stays as written: dropping prose is worse
			// than keeping a marker a reader can look up.
			return text
		}
		text = replacement + strings.TrimSpace(rest)
		if text == "" {
			return ""
		}
	}
	return strings.Join(strings.Fields(text), " ")
}

// splitCommand splits a leading "\word" or "@word" command from its text, with
// an optional [in]/[out]/[in,out] parameter direction.
func splitCommand(text string) (word, rest string, ok bool) {
	if text == "" || (text[0] != '\\' && text[0] != '@') {
		return "", "", false
	}
	end := 1
	for end < len(text) && (text[end] >= 'a' && text[end] <= 'z' || text[end] >= 'A' && text[end] <= 'Z') {
		end++
	}
	if end == 1 {
		return "", "", false
	}
	word = strings.ToLower(text[1:end])
	rest = text[end:]
	if strings.HasPrefix(rest, "[") {
		if close := strings.IndexByte(rest, ']'); close >= 0 {
			rest = rest[close+1:]
		}
	}
	// "\param name text" names the parameter first; the marker is dropped and
	// the name stays, so the reader still knows which parameter is described.
	if word == "param" || word == "tparam" || word == "arg" {
		return word, strings.TrimSpace(rest), true
	}
	return word, strings.TrimSpace(rest), true
}

// docBlock is a declaration's Go doc comment, with the "//" prefix and without
// a trailing newline. fallback is the generated contract sentence, used when the
// header has no doc comment; when it has one, the comment follows the sentence
// as its own paragraph unless it already opens with the identifier.
func docBlock(goName, fallback, raw string) string {
	doc := docComment(raw)
	if doc == "" {
		return "// " + fallback
	}
	if firstWord(doc) == goName {
		return prefixed(doc)
	}
	if fallback == "" {
		return prefixed(doc)
	}
	return "// " + fallback + "\n//\n" + prefixed(doc)
}

// firstWord is the first whitespace-delimited word of a doc text.
func firstWord(doc string) string {
	fields := strings.Fields(doc)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimRight(fields[0], ".:,;")
}

// prefixed renders a doc text as Go comment lines.
func prefixed(doc string) string {
	lines := strings.Split(terminate(doc), "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = "//"
			continue
		}
		lines[i] = "// " + line
	}
	return strings.Join(lines, "\n")
}

// terminate closes a paragraph that ends without punctuation. gofmt reads a
// one-line paragraph that starts with a capital and ends without punctuation as
// a heading when another paragraph follows it, which a doc comment never means;
// a period is the smallest thing that keeps the prose prose.
func terminate(doc string) string {
	paragraphs := strings.Split(doc, "\n\n")
	for i, paragraph := range paragraphs {
		lines := strings.Split(paragraph, "\n")
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" {
			continue
		}
		switch last[len(last)-1] {
		case '.', '!', '?', ':', ';', ')', ']':
			continue
		}
		lines[len(lines)-1] += "."
		paragraphs[i] = strings.Join(lines, "\n")
	}
	return strings.Join(paragraphs, "\n\n")
}

// commentLines renders a declaration's doc text as indented continuation lines
// of an existing doc comment: every line becomes a "//" line, and an empty text
// renders nothing. It is how an enum constant carries its own description.
func commentLines(raw string) string {
	doc := docComment(raw)
	if doc == "" {
		return ""
	}
	return prefixed(doc) + "\n"
}

// indentLines prefixes every line of a comment block with indent. It exists for
// a doc comment written inside a declaration group, where the comment must be
// indented by one tab to belong to the grouped declaration.
func indentLines(block, indent string) string {
	lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n") + "\n"
}
