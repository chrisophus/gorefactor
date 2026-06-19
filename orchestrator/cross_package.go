package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// CrossPackageOperationHandler manages cross-package refactoring operations
type CrossPackageOperationHandler struct {
	fset *token.FileSet
}

// NewCrossPackageOperationHandler creates a new handler
func NewCrossPackageOperationHandler() *CrossPackageOperationHandler {
	return &CrossPackageOperationHandler{
		fset: token.NewFileSet(),
	}
}

// CrossPackageMoveReport describes what a cross-package move did.
type CrossPackageMoveReport struct {
	SourceFile       string   `json:"sourceFile"`
	DestFile         string   `json:"destFile"`
	FuncName         string   `json:"funcName"`
	SourcePackage    string   `json:"sourcePackage"`
	DestPackage      string   `json:"destPackage"`
	SourceImportPath string   `json:"sourceImportPath"`
	DestImportPath   string   `json:"destImportPath"`
	QualifiedSymbols []string `json:"qualifiedSymbols,omitempty"` // source symbols qualified inside the moved code
	RewrittenFiles   []string `json:"rewrittenFiles,omitempty"`   // call-site files updated
	SourceStartLine  int      `json:"sourceStartLine"`
	SourceEndLine    int      `json:"sourceEndLine"`
	DeclCode         string   `json:"-"`
}

// crossPackageMove holds the analyzed state of one cross-package move between
// the planning (read-only) and apply (mutating) phases.
type crossPackageMove struct {
	fset       *token.FileSet
	sourceFile string
	destFile   string
	funcName   string
	srcDir     string
	destDir    string

	srcNode *ast.File     // parsed source file
	fn      *ast.FuncDecl // the declaration being moved

	srcPkgName  string
	destPkgName string
	srcImport   string
	destImport  string

	unexportedRefs []string // source unexported symbols the moved code references
	exportedRefs   []string // source exported symbols to qualify in the moved code
	samePkgSites   []CallSiteRef
	externalSites  []CallSiteRef

	srcImportsDest bool // source package already imports the destination
	destImportsSrc bool // destination package already imports the source

	report *CrossPackageMoveReport
}

// CallSiteRef is a file:line reference to a call of the moved function.
type CallSiteRef struct {
	File string
	Line int
	Pkg  string
}

func (c CallSiteRef) String() string { return fmt.Sprintf("%s:%d", c.File, c.Line) }

// check fails loudly when the move would break the build, listing the
// affected call sites or symbols so the caller can act on them.
func (mv *crossPackageMove) check() error {
	if len(mv.unexportedRefs) > 0 {
		return fmt.Errorf(
			"cannot move %s from package %s to package %s: it references unexported package symbols that would be out of reach: %s\nexport those symbols or move them along first",
			mv.funcName, mv.srcPkgName, mv.destPkgName, strings.Join(mv.unexportedRefs, ", "))
	}
	if !ast.IsExported(mv.funcName) {
		sites := append(append([]CallSiteRef{}, mv.samePkgSites...), mv.externalSites...)
		if len(sites) > 0 {
			return fmt.Errorf(
				"cannot move unexported function %s from package %s to package %s: it would be unreachable from %d call site(s):\n%s\nexport the function (rename it) or move the callers too",
				mv.funcName, mv.srcPkgName, mv.destPkgName, len(sites), formatCallSites(sites))
		}
	}
	needSrcInDest := len(mv.exportedRefs) > 0
	needDestInSrc := len(mv.samePkgSites) > 0
	if (needSrcInDest && (needDestInSrc || mv.srcImportsDest)) ||
		(needDestInSrc && mv.destImportsSrc) {
		return fmt.Errorf(
			"cannot move %s from %s to %s: the move would create an import cycle between %s and %s (moved code references source symbols %v; source-package call sites:\n%s)",
			mv.funcName, mv.sourceFile, mv.destFile, mv.srcImport, mv.destImport,
			mv.exportedRefs, formatCallSites(mv.samePkgSites))
	}
	return nil
}

func formatCallSites(sites []CallSiteRef) string {
	lines := make([]string, 0, len(sites))
	for _, s := range sites {
		lines = append(lines, "  "+s.String())
	}
	if len(lines) == 0 {
		return "  (none)"
	}
	return strings.Join(lines, "\n")
}

// analyzeMovedDeclRefs records which source package-level symbols the moved
// declaration references, split into exported (will be qualified) and
// unexported (blockers).
func (mv *crossPackageMove) analyzeMovedDeclRefs() error {
	symbols, err := packageLevelSymbols(mv.fset, mv.srcDir, mv.srcPkgName)
	if err != nil {
		return err
	}
	delete(symbols, mv.funcName)

	skip := nonReferenceIdents(mv.fn)
	seen := map[string]bool{}
	ast.Inspect(mv.fn, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || skip[id] || seen[id.Name] || identResolvesWithin(id, mv.fn) {
			return true
		}
		if _, isPkgSymbol := symbols[id.Name]; !isPkgSymbol {
			return true
		}
		seen[id.Name] = true
		if ast.IsExported(id.Name) {
			mv.exportedRefs = append(mv.exportedRefs, id.Name)
		} else {
			mv.unexportedRefs = append(mv.unexportedRefs, id.Name)
		}
		return true
	})
	return nil
}

// findCallSites locates every call site of the moved function: bare
// references inside the source package and qualified references across the
// module.
func (mv *crossPackageMove) findCallSites(modRoot string) error {
	// Same-package references.
	files, err := packageGoFiles(mv.srcDir)
	if err != nil {
		return err
	}
	for _, path := range files {
		node, err := parser.ParseFile(mv.fset, path, nil, parser.ParseComments)
		if err != nil {
			continue // unparseable neighbors are not this operation's problem
		}
		if node.Name.Name != mv.srcPkgName {
			continue
		}
		skip := nonReferenceIdents(node)
		ast.Inspect(node, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != mv.funcName || skip[id] {
				return true
			}
			// A reference either resolves to the moved declaration (same
			// file) or is unresolved (declared in a sibling file).
			if id.Obj != nil && id.Obj.Decl != nil {
				if fn, isFn := id.Obj.Decl.(*ast.FuncDecl); !isFn || fn.Name.Name != mv.funcName || fn.Recv != nil {
					return true // shadowed by a local declaration
				}
			}
			mv.samePkgSites = append(mv.samePkgSites, CallSiteRef{
				File: path,
				Line: mv.fset.Position(id.Pos()).Line,
				Pkg:  mv.srcPkgName,
			})
			return true
		})
	}

	// Qualified references elsewhere in the module.
	sites, err := findQualifiedReferences(mv.fset, modRoot, mv.srcDir, mv.srcImport, mv.srcPkgName, mv.funcName)
	if err != nil {
		return err
	}
	mv.externalSites = sites
	return nil
}

// ErrFileNotFound indicates file doesn't exist
var ErrFileNotFound = fmt.Errorf("file not found")
