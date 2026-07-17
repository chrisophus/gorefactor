package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

// blockNameStopwords are dropped when slugifying a leading comment into a
// helper name, so "Parse the optional configuration flags" becomes
// parseOptionalConfigurationFlags rather than parseTheOptionalConfiguration.
var blockNameStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "for": true, "to": true, "of": true,
	"in": true, "on": true, "and": true, "or": true, "with": true, "this": true,
	"that": true, "these": true, "those": true, "its": true, "it": true,
	"into": true, "from": true, "by": true, "as": true, "we": true, "then": true,
}

// blockNameMaxWords caps how many comment words feed the generated identifier —
// enough to stay descriptive, few enough to stay readable.
const blockNameMaxWords = 4

// SuggestBlockName derives a readable helper name for an extracted block.
// Priority: (1) the block's leading comment, slugified to a verb-first
// camelCase identifier; (2) the block's structure (the variable it computes or
// the collection it ranges over); (3) a positional fallback. used tracks names
// already handed out for this parent so two blocks never collide; a duplicate
// gets a numeric suffix.
func SuggestBlockName(stmt ast.Stmt, cmap ast.CommentMap, fallbackLine int, used map[string]bool) string {
	name := nameFromComment(stmt, cmap)
	if name == "" {
		name = nameFromStructure(stmt)
	}
	if name == "" {
		name = fmt.Sprintf("extractBlockL%d", fallbackLine)
	}
	name = ensureUnique(name, used)
	if used != nil {
		used[name] = true
	}
	return name
}

// nameFromComment turns a block's leading comment into an identifier.
func nameFromComment(stmt ast.Stmt, cmap ast.CommentMap) string {
	if cmap == nil {
		return ""
	}
	groups := cmap[stmt]
	if len(groups) == 0 {
		return ""
	}
	return slugToCamel(firstSentence(groups[0].Text()))
}

// nameFromStructure derives a name from the statement's shape when there is no
// usable comment: an assignment is named after the variable it computes, a
// range loop after the collection it iterates.
func nameFromStructure(stmt ast.Stmt) string {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Lhs) > 0 {
			if id, ok := s.Lhs[0].(*ast.Ident); ok && id.Name != "_" && id.Name != "" {
				return "compute" + upperFirst(id.Name)
			}
		}
	case *ast.RangeStmt:
		if id, ok := s.X.(*ast.Ident); ok {
			return "process" + upperFirst(id.Name)
		}
	}
	return ""
}

// slugToCamel converts a phrase into a verb-first lowerCamelCase identifier,
// dropping stopwords and capping the word count. Returns "" if nothing usable
// survives.
func slugToCamel(phrase string) string {
	fields := strings.FieldsFunc(phrase, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var words []string
	for _, w := range fields {
		lw := strings.ToLower(w)
		if blockNameStopwords[lw] {
			continue
		}
		words = append(words, w)
		if len(words) >= blockNameMaxWords {
			break
		}
	}
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.ToLower(w))
			continue
		}
		b.WriteString(upperFirst(strings.ToLower(w)))
	}
	name := b.String()
	// An identifier must start with a letter; if the first word began with a
	// digit, prefix a verb so the result is still valid Go.
	if r := []rune(name)[0]; !unicode.IsLetter(r) {
		name = "do" + upperFirst(name)
	}
	if token.Lookup(name).IsKeyword() {
		name += "Block"
	}
	return name
}

// upperFirst upper-cases the first rune of an ASCII identifier word.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// firstSentence trims a comment to its first sentence or line.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, ".\n"); i >= 0 {
		text = text[:i]
	}
	return text
}

// ensureUnique appends a numeric suffix while name is already in used.
func ensureUnique(name string, used map[string]bool) string {
	if used == nil || !used[name] {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", name, i)
		if !used[candidate] {
			return candidate
		}
	}
}
