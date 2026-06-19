package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
)

func findCallersCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: find-callers <Func|Receiver:Method> [--in path] [--json]")
	}
	target := args[0]
	root := "."
	jsonOut := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--in":
			if i+1 < len(args) {
				root = args[i+1]
				i++
			}
		case "--json":
			jsonOut = true
		}
	}
	name, recv := splitNameReceiver(target)
	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	ca := analyzer.NewCallAnalyzer(files)
	res, err := ca.FindCallers(name, recv)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Printf("Target: %s%s (defined at %s:%d, exported=%v)\n", recvPrefix(recv), name, res.TargetFile, res.TargetLine, res.IsExported)
	fmt.Printf("Total callers: %d  (direct=%d  indirect=%d  test=%d)\n",
		res.TotalCallCount, len(res.DirectCallers), len(res.IndirectCallers), len(res.TestCallers))
	if len(res.DirectCallers) > 0 {
		fmt.Println("\nDirect callers:")
		for _, c := range res.DirectCallers {
			fmt.Printf("  %s:%d  %s%s\n", c.File, c.Line, recvPrefix(c.CallerReceiver), c.CallerName)
		}
	}
	if len(res.TestCallers) > 0 {
		fmt.Println("\nTest callers:")
		for _, c := range res.TestCallers {
			fmt.Printf("  %s:%d  %s\n", c.File, c.Line, c.CallerName)
		}
	}
	if len(res.IndirectCallers) > 0 {
		fmt.Println("\nIndirect callers (via interface):")
		for _, c := range res.IndirectCallers {
			fmt.Printf("  %s:%d  %s  (%s)\n", c.File, c.Line, c.CallerName, c.IndirectType)
		}
	}
	return nil
}

func findUsesCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: find-uses <Symbol|Receiver:Method> [--in path] [--json]")
	}
	target := args[0]
	root := "."
	jsonOut := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--in":
			if i+1 < len(args) {
				root = args[i+1]
				i++
			}
		case "--json":
			jsonOut = true
		}
	}
	name, recv := splitNameReceiver(target)
	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	ua := analyzer.NewUseAnalyzer(files)
	uses, err := ua.FindAllUses(analyzer.SymbolQuery{Name: name, Receiver: recv})
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(uses)
	}
	fmt.Printf("%d use(s) of %s%s:\n", len(uses), recvPrefix(recv), name)
	for _, u := range uses {
		fmt.Printf("  %s:%d  [%s]  %s\n", u.File, u.Line, u.Context, u.Snippet)
	}
	return nil
}

func findImplementationsCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: find-implementations <InterfaceName> [--in path] [--json]")
	}
	target := args[0]
	root := "."
	jsonOut := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--in":
			if i+1 < len(args) {
				root = args[i+1]
				i++
			}
		case "--json":
			jsonOut = true
		}
	}
	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	ia := analyzer.NewInterfaceAnalyzer(files)
	res, err := ia.FindImplementations(target)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Printf("Interface %s (%d methods, defined at %s:%d):\n", res.Interface.Name, len(res.Interface.Methods), res.Interface.File, res.Interface.Line)
	for _, m := range res.Interface.Methods {
		fmt.Printf("  %s%s\n", m.Name, m.Signature)
	}
	fmt.Printf("\n%d implementation(s):\n", len(res.Implementations))
	for _, impl := range res.Implementations {
		fmt.Printf("  %s  (%s:%d)\n", impl.TypeName, impl.File, impl.Line)
	}
	return nil
}

func splitNameReceiver(s string) (name, receiver string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[i+1:], s[:i]
		}
	}
	return s, ""
}

func recvPrefix(recv string) string {
	if recv == "" {
		return ""
	}
	return recv + "."
}

// Output all packages and their imports as JSON

// Check for cycles
