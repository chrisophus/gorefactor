package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// IsGeneratedFallbackName reports whether name is fallback-quality: either the positional
// extractBlockL<line> form (SuggestBlockName found no comment or recognizable structure to name the
// block) or a collision-suffixed form like processStmts2 (the meaningful name was already taken, so
// the suffixed one no longer describes the block). Such blocks make poor auto-extraction targets:
// lifting them into a many-parameter helper under a non-descriptive name hurts readability, so the
// autofix path skips them and leaves the finding for a human.
func IsGeneratedFallbackName(name string) bool {
	if name == "" {
		return false
	}
	const prefix = "extractBlockL"
	if strings.HasPrefix(name, prefix) && allDigits(strings.TrimPrefix(name, prefix)) {
		return true
	}
	// A trailing digit means ensureUnique had to suffix the name because the
	// meaningful form was already taken (processStmts -> processStmts2). The
	// suffixed form no longer describes the block, so it is fallback-quality
	// for autofix purposes.
	return allDigits(name[len(name)-1:])

}

// PackageFuncNames returns the set of top-level function/method names declared
// across every .go file in the same directory as filePath. Seeding a naming
// run's `used` set with this prevents a generated helper name — especially the
// positional extractBlockL<line> fallback — from colliding with a function that
// already exists elsewhere in the package (a real bug: two files with a block
// at the same line both minted extractBlockL<line>, redeclaring it).
func PackageFuncNames(filePath string) map[string]bool {
	names := map[string]bool{}
	dir := filepath.Dir(filePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				names[fn.Name.Name] = true
			}
		}
	}
	return names
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
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
		if label := exprLabel(s.X); label != "" {
			return "process" + upperFirst(label)
		}
	case *ast.ForStmt:
		// A counted loop is usually iterating a collection; name it after the
		// len(coll) that bounds it, e.g. `for i:=0; i<len(args); i++` -> processArgs.
		if coll := loopCollection(s.Cond); coll != "" {
			return "process" + upperFirst(coll)
		}
	case *ast.SwitchStmt:
		if label := exprLabel(s.Tag); label != "" {
			return "handle" + upperFirst(label)
		}
	case *ast.TypeSwitchStmt:
		return "handleTypeSwitch"
	}
	return ""
}

// exprLabel returns a short identifier-ish label for an expression used as a
// naming hint: a plain name, the last field of a selector chain (joined with
// the one before it when that adds meaning), or the base of an index/call.
func exprLabel(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		// Prefer the last two segments (`call.Function.Name` -> FunctionName)
		// so the label stays distinctive; fall back to the field alone.
		if inner, ok := e.X.(*ast.SelectorExpr); ok {
			return inner.Sel.Name + upperFirst(e.Sel.Name)
		}
		return e.Sel.Name
	case *ast.IndexExpr:
		return exprLabel(e.X)
	case *ast.CallExpr:
		return exprLabel(e.Fun)
	}
	return ""
}

// loopCollection finds a `len(X)` operand in a for-loop condition and returns
// the label of X, so counted loops can be named after what they iterate.
func loopCollection(cond ast.Expr) string {
	var found string
	ast.Inspect(cond, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "len" && len(call.Args) == 1 {
			if label := exprLabel(call.Args[0]); label != "" {
				found = label
				return false
			}
		}
		return true
	})
	return found
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
