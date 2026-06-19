package main

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/packages"
)

func buildAddParamEdits(pkgs []*packages.Package, tgt *sigTarget, params []sigParam, action *sigAction, locator string) ([]textEdit, string, error) {
	for _, p := range params {
		if p.name == action.paramName {
			return nil, "", usageErrorf("parameter %q already exists on %s", action.paramName, locator)
		}
	}
	if rn := receiverName(tgt.fn); rn != "" && rn == action.paramName {
		return nil, "", usageErrorf("parameter name %q collides with the receiver name", action.paramName)
	}
	arity := len(params)
	variadicIndex := variadicIndexOf(params)
	idx := action.position
	if idx == -1 {
		if variadicIndex >= 0 {
			return nil, "", usageErrorf("%s is variadic; pass --position N with N <= %d (before the variadic parameter)", locator, variadicIndex)
		}
		idx = arity
	}
	if idx > arity {
		return nil, "", usageErrorf("--position %d out of range (0-%d)", idx, arity)
	}
	if variadicIndex >= 0 && idx > variadicIndex {
		return nil, "", usageErrorf("--position %d would place the parameter after the variadic parameter (max %d)", idx, variadicIndex)
	}
	value, err := callValueFor(tgt, action)
	if err != nil {
		return nil, "", err
	}
	if conflicts := interfaceConflicts(pkgs, tgt.recv, tgt.fn.Name.Name); len(conflicts) > 0 {
		return nil, "", notFoundErrorf(
			"%s satisfies interface(s) declaring this method; changing the signature would break satisfaction:\n  %s\nupdate the interface(s) first",
			locator, strings.Join(conflicts, "\n  "))
	}
	sites, badRefs := gatherFuncRefs(pkgs, tgt.fn.Name.Pos())
	if err := checkRewriteSafety(locator, badRefs, sites, arity, variadicIndex); err != nil {
		return nil, "", err
	}

	parts := paramTexts(tgt.pkg.Fset, params, true)
	newPart := action.paramName + " " + action.paramType
	parts = append(parts[:idx], append([]string{newPart}, parts[idx:]...)...)
	edits := []textEdit{signatureEdit(tgt, parts)}
	for _, s := range sites {
		edits = append(edits, insertArgEdit(s, idx, value))
	}
	detail := fmt.Sprintf("Added parameter %q to %s at position %d; updated %d call site(s) in %d file(s) with %s",
		newPart, locator, idx, len(sites), countSiteFiles(sites), value)
	return edits, detail, nil
}

func buildRemoveParamEdits(pkgs []*packages.Package, tgt *sigTarget, params []sigParam, action *sigAction, locator string) ([]textEdit, string, error) {
	idx, err := resolveParamRef(params, action.removeRef, locator)
	if err != nil {
		return nil, "", err
	}
	target := params[idx]
	if target.variadic {
		return nil, "", usageErrorf("removing the variadic parameter is not supported")
	}
	if target.nameIdent != nil && target.name != "_" {
		if _, locs := paramUseEdits(tgt, target.nameIdent.Pos(), ""); len(locs) > 0 {
			return nil, "", notFoundErrorf(
				"parameter %q is used in the body of %s; remove these uses first:\n  %s",
				target.name, locator, strings.Join(locs, "\n  "))
		}
	}
	if conflicts := interfaceConflicts(pkgs, tgt.recv, tgt.fn.Name.Name); len(conflicts) > 0 {
		return nil, "", notFoundErrorf(
			"%s satisfies interface(s) declaring this method; changing the signature would break satisfaction:\n  %s\nupdate the interface(s) first",
			locator, strings.Join(conflicts, "\n  "))
	}
	arity := len(params)
	variadicIndex := variadicIndexOf(params)
	sites, badRefs := gatherFuncRefs(pkgs, tgt.fn.Name.Pos())
	if err := checkRewriteSafety(locator, badRefs, sites, arity, variadicIndex); err != nil {
		return nil, "", err
	}

	rest := append(append([]sigParam{}, params[:idx]...), params[idx+1:]...)
	edits := []textEdit{signatureEdit(tgt, paramTexts(tgt.pkg.Fset, rest, false))}
	for _, s := range sites {
		edits = append(edits, removeArgEdit(s, idx))
	}
	name := target.name
	if name == "" {
		name = fmt.Sprintf("#%d", idx)
	}
	detail := fmt.Sprintf("Removed parameter %q from %s; updated %d call site(s) in %d file(s)",
		name, locator, len(sites), countSiteFiles(sites))
	return edits, detail, nil
}
