package main

import "fmt"

// applySplitFile runs `gorefactor split <file>` and returns a compact result.
func applySplitFile(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	out, err := runIn(".", gorefactorBin(), "split", file)
	if err != nil {
		return "ERROR splitting file: " + trim(out, 400)
	}
	return "split " + file + ": " + trim(out, 300)
}

// applyWrapErrors runs `gorefactor wrap-errors <file> <function>`.
func applyWrapErrors(file, function string) string {
	if file == "" || function == "" {
		return "ERROR: 'file' and 'function' are required"
	}
	out, err := runIn(".", gorefactorBin(), "wrap-errors", file, function)
	if err != nil {
		return "ERROR wrapping errors: " + trim(out, 400)
	}
	return trim(out, 400)
}

// applySetDoc runs `gorefactor set-doc <file> <decl> -` with the doc text piped in.
func applySetDoc(file, decl, doc string) string {
	if file == "" || decl == "" || doc == "" {
		return "ERROR: 'file', 'declaration', and 'doc' are all required"
	}
	// set-doc reads content from stdin when the last arg is "-"
	out, err := runInWithStdin(".", doc, gorefactorBin(), "set-doc", file, decl, "-")
	if err != nil {
		return "ERROR setting doc: " + trim(out, 400)
	}
	return fmt.Sprintf("set doc on %s in %s", decl, file)
}

// applyExtractMethod runs `gorefactor extract <file> <start> <end> <name>` and
// returns a compact result string the model can react to.
func applyExtractMethod(file, start, end, name string) string {
	if file == "" || start == "" || end == "" || name == "" {
		return "ERROR: file, start_line, end_line, and new_function_name are all required"
	}
	out, err := runIn(".", gorefactorBin(), "extract", file, start, end, name)
	if err != nil {
		return "ERROR extracting method: " + trim(out, 400)
	}
	return fmt.Sprintf("extracted lines %s-%s into %s in %s", start, end, name, file)
}
