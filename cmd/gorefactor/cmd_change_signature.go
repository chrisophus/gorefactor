package main

import (
	"go/token"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/refactor/changesig"
)

var changeSignatureFlags = mutFlagSpec(map[string]bool{
	"--add-param":      true,
	"--position":       true,
	"--call-value":     true,
	"--remove-param":   true,
	"--rename-param":   true,
	"--reorder-params": true,
	"--change-returns": true,
})

func init() {
	registerCommand(Command{
		Name:        "change-signature",
		Mutates:     true,
		MCPTool:     true,
		TxnSafe:     true,
		Description: "Change a function/method signature and update all call sites (add/remove/rename a parameter)",
		Usage:       `change-signature <file> <Func|Receiver:Method> (--add-param "name type" [--position N] [--call-value EXPR] | --remove-param <name|index> | --rename-param <old> <new>) [--json] [--dry-run] [--gate]`,
		MinArgs:     2,
		MaxArgs:     3,
		Flags:       changeSignatureFlags,
		Run:         changeSignatureCommand,
	})
}

// parseSignatureAction turns the parsed flags into a changesig.Action. Flag
// parsing (and its usage errors) stay in the CLI; the engine consumes the
// resulting Action.
func parseSignatureAction(flags map[string]string, pos []string) (*changesig.Action, error) {
	if flags["--reorder-params"] != "" {
		return nil, usageErrorf("--reorder-params is not supported (out of scope for change-signature)")
	}
	if flags["--change-returns"] != "" {
		return nil, usageErrorf("--change-returns is not supported (out of scope for change-signature)")
	}
	a := &changesig.Action{Position: -1}
	count := 0
	if v := flags["--add-param"]; v != "" {
		count++
		if err := parseSigAddParam(a, v); err != nil {
			return nil, err
		}
	}
	if v := flags["--remove-param"]; v != "" {
		count++
		a.Kind = "remove"
		a.RemoveRef = v
	}
	if v := flags["--rename-param"]; v != "" {
		count++
		if err := parseSigRenameParam(a, v, pos); err != nil {
			return nil, err
		}
	}
	if count != 1 {
		return nil, usageErrorf("change-signature wants exactly one of --add-param, --remove-param, --rename-param")
	}
	if err := parseSigActionModifiers(a, flags); err != nil {
		return nil, err
	}
	return a, nil
}

func parseSigAddParam(a *changesig.Action, v string) error {
	a.Kind = "add"
	fields := strings.Fields(v)
	if len(fields) < 2 {
		return usageErrorf(`--add-param wants "name type" (e.g. --add-param "ctx context.Context")`)
	}
	a.ParamName = fields[0]
	a.ParamType = strings.Join(fields[1:], " ")
	if !token.IsIdentifier(a.ParamName) {
		return usageErrorf("invalid parameter name %q", a.ParamName)
	}
	return nil
}

func parseSigRenameParam(a *changesig.Action, v string, pos []string) error {
	a.Kind = "rename"
	old, newName := v, ""
	for _, sep := range []string{"=", ",", " "} {
		if i := strings.Index(v, sep); i >= 0 {
			old, newName = v[:i], v[i+len(sep):]
			break
		}
	}
	if newName == "" && len(pos) >= 3 {
		newName = pos[2]
	}
	if old == "" || newName == "" {
		return usageErrorf("--rename-param wants <old> <new> (or \"old=new\")")
	}
	if !token.IsIdentifier(newName) {
		return usageErrorf("invalid parameter name %q", newName)
	}
	a.OldName, a.NewName = strings.TrimSpace(old), strings.TrimSpace(newName)
	return nil
}

func parseSigActionModifiers(a *changesig.Action, flags map[string]string) error {
	if v, ok := flags["--position"]; ok {
		if a.Kind != "add" {
			return usageErrorf("--position only applies to --add-param")
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return usageErrorf("--position wants a non-negative integer, got %q", v)
		}
		a.Position = n
	}
	if v, ok := flags["--call-value"]; ok {
		if a.Kind != "add" {
			return usageErrorf("--call-value only applies to --add-param")
		}
		a.CallValue = v
	}
	return nil
}

func changeSignatureCommand(args []string) error {
	pos, flags := parseFlags(args, changeSignatureFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: change-signature <file> <Func|Receiver:Method> --add-param|--remove-param|--rename-param ...")
	}
	file, locator := pos[0], pos[1]
	m := &mutation{op: "change-signature", file: file}
	m.setCommonFlags(flags)

	action, err := parseSignatureAction(flags, pos)
	if err != nil {
		return m.fail(err)
	}
	edits, detail, err := changesig.Plan(file, locator, action)
	if err != nil {
		return m.fail(err)
	}
	m.files = changesig.EditedFiles(edits)
	return m.run(func() (string, error) {
		if err := changesig.Apply(edits); err != nil {
			return "", err
		}
		return detail, nil
	})
}
