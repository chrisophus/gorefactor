package main

import (
	"sort"

	"github.com/chrisophus/gorefactor/analyzer"
)

// buildContextPack assembles the context sections highest-value-first and
// trims them to the character budget.
func buildContextPack(target, root string, budget int) (*contextPack, error) {
	name, recv := splitNameReceiver(target)
	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return nil, err
	}

	ua := analyzer.NewUseAnalyzer(files)
	def, err := ua.FindSymbolDefinition(analyzer.SymbolQuery{Name: name, Receiver: recv})
	if err != nil {
		return nil, notFoundErrorf("symbol %q not found under %s", target, root)
	}

	pack := &contextPack{Symbol: target, File: def.File, Line: def.Line, Budget: budget}

	source, doc, fnType := extractDeclContext(def.File, name, recv)
	pack.Doc = doc
	pack.Definition = source
	if pack.Definition == "" {
		pack.Definition = def.Snippet
	}

	// Callers (functions/methods only; types have no call sites).
	ca := analyzer.NewCallAnalyzer(files)
	if res, cerr := ca.FindCallers(name, recv); cerr == nil {
		for _, c := range res.DirectCallers {
			pack.Callers = append(pack.Callers, contextCaller{
				Name:      c.CallerName,
				Signature: callerSignature(c.File, c.CallerName, c.CallerReceiver),
				File:      c.File,
				Line:      c.Line,
				Context:   sourceContext(c.File, c.Line, 2),
			})
		}
		tests := map[string]bool{}
		for _, c := range res.TestCallers {
			tests[c.CallerName] = true
		}
		for n := range tests {
			pack.Tests = append(pack.Tests, n)
		}
		sort.Strings(pack.Tests)
	}

	// Definitions of named types used in the symbol's signature.
	if fnType != nil {
		for _, tn := range signatureTypeNames(fnType) {
			tdef, terr := ua.FindSymbolDefinition(analyzer.SymbolQuery{Name: tn})
			if terr != nil {
				continue
			}
			src, _, _ := extractDeclContext(tdef.File, tn, "")
			if src == "" {
				src = tdef.Snippet
			}
			pack.Types = append(pack.Types, contextTypeDef{Name: tn, File: tdef.File, Line: tdef.Line, Source: src})
		}
	}

	trimContextPack(pack, budget)
	return pack, nil
}
