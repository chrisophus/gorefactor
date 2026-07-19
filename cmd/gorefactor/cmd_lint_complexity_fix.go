package main

import (
	"github.com/chrisophus/gorefactor/analyzer"
)

// reduceComplexityByExtraction applies the extractions recommended for function
// in file and returns how many blocks were successfully extracted. It is shared
// by the complexity autofix and `recommend --reduce-complexity --apply`.
// allowReturns forwards --allow-returns to the extract engine so aggressive
// runs can lift return-bearing blocks. (Signature edited directly:
// change-signature requires a type-checking module, which the new body
// prevents mid-edit.)
func reduceComplexityByExtraction(file, function string, threshold int, allowReturns bool) (int, error) {
	res, err := analyzer.RecommendComplexityReduction(file, function, threshold)
	return applyRecommendedExtractions(file, res.Extractions, err, allowReturns, func(e analyzer.ComplexityExtraction) extractionSpec {
		return extractionSpec{StartLine: e.StartLine, EndLine: e.EndLine, Suggestion: e.Suggestion}
	})

}
