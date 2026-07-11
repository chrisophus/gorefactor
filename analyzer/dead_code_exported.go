package analyzer

// DetectDeadExportedFunctions is the aggressive companion to
// DetectDeadFunctions: it reports exported top-level functions whose name
// appears nowhere in the given file set (normally the whole module) outside
// their own declaration. Exported symbols are skipped by the safe detector
// because callers outside the package are invisible to a per-package scan;
// counting identifiers module-wide closes that gap for everything except
// consumers outside the module, which is why findings from this scan must
// only be deleted under a build+test verify gate — and why it is offered at
// the aggressive fix level only.
//
// Methods are excluded entirely: exported methods can satisfy interfaces
// invoked via reflection (encoding/json's MarshalJSON, fmt's String) with no
// same-name identifier anywhere in the module, so deleting them is not safe
// even aggressively.
func DetectDeadExportedFunctions(files []string) []DeadCodeIssue {
	detector := NewDeadCodeDetector(files)
	moduleFreq := detector.identFrequency()

	var issues []DeadCodeIssue
	for _, f := range files {
		for _, fn := range extractFunctions(f) {
			if fn.Receiver != "" || !fn.IsExported || fn.IsMain() || fn.IsInit() || fn.IsTest() {
				continue
			}
			if moduleFreq[fn.Name] > 1 {
				continue
			}
			issues = append(issues, DeadCodeIssue{
				Type:       "function",
				Name:       fn.Name,
				File:       fn.File,
				Line:       fn.Line,
				IsExported: true,
				Reason:     "Never referenced in module",
			})
		}
	}
	return issues
}
