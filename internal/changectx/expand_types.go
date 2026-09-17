package changectx

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// siblingsPerInterface caps how many other implementations one interface
// contributes. A widely implemented interface would otherwise answer a
// two-line change with every type in the module.
const siblingsPerInterface = 12

// expandTypes emits the definition of each named type a changed signature
// mentions. A signature the reviewer cannot resolve is a signature they have
// to guess at.
func (b *builder) expandTypes() {
	seen := map[token.Pos]bool{}
	for _, d := range b.decls {
		if d.obj == nil {
			continue
		}
		switch t := d.obj.Type().(type) {
		case *types.Signature:
			b.addTypes(d, signatureNamed(t), "signature", seen)
			// What the body names, which a signature does not reach. A change
			// that starts constructing a different type, or asserting to one,
			// is judged against that type and the signature never mentions it.
			b.addTypes(d, b.bodyNamed(d), "body", seen)
		default:
			// A changed type, var or const. Its own referents were never
			// walked: the stage only ever looked at signatures, so editing a
			// struct brought none of the types of its fields.
			//
			// A type declaration is walked through its underlying type. The
			// walker stops at a named type, and a named type's own name is the
			// declaration being changed, so walking it directly finds only
			// itself.
			referent := t
			if _, isType := d.obj.(*types.TypeName); isType {
				referent = t.Underlying()
			}
			b.addTypes(d, namedIn(referent), "declared", seen)
		}
	}
}

// addTypes emits the declarations of named types, once each across the whole
// change, skipping the ones the change already carries.
//
// via says how the type was reached, because the three are worth different
// amounts: a type in a signature is part of the contract, one in a body is what
// the code works with, and one a changed type refers to is its shape.
func (b *builder) addTypes(d *decl, named []*types.Named, via string, seen map[token.Pos]bool) {
	kept := 0
	for _, n := range named {
		obj := n.Obj()
		if obj == nil || obj.Pkg() == nil || seen[obj.Pos()] {
			continue
		}
		target := b.declFor(obj)
		if target == nil || b.isChanged(target) {
			continue // its own declaration already carries the change
		}
		if kept >= typesPerDecl {
			b.notes = append(b.notes, fmt.Sprintf(
				"further %s type(s) of %s were not expanded (cap %d per declaration)",
				via, d.scope, typesPerDecl))
			return
		}
		seen[obj.Pos()] = true
		kept++
		b.add(Expansion{
			Role:      RoleType,
			Priority:  priorityFor(d),
			Symbol:    target.symbol,
			Scope:     target.scope,
			File:      target.rel,
			StartLine: target.start,
			EndLine:   target.end,
			Content:   b.slice(target.rel, target.start, target.end),
			Details: map[string]string{
				"kind":         "type",
				"referencedBy": d.scope,
				"via":          via,
			},
		})
	}
}

// bodyNamed collects the named types a changed function's body mentions, found
// through the type checker rather than by reading the syntax: a type name in a
// body is an identifier that resolves to a TypeName, wherever it sits.
func (b *builder) bodyNamed(d *decl) []*types.Named {
	if d.fn == nil || d.fn.Body == nil {
		return nil
	}
	info := b.typesInfoFor(d)
	if info == nil {
		return nil
	}
	var out []*types.Named
	seen := map[*types.Named]bool{}
	ast.Inspect(d.fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		tn, ok := info.Uses[id].(*types.TypeName)
		if !ok {
			return true
		}
		named, ok := tn.Type().(*types.Named)
		if !ok || seen[named] {
			return true
		}
		seen[named] = true
		out = append(out, named)
		return true
	})
	return out
}

// typesPerDecl caps how many types one changed declaration contributes under
// one route. A body that names thirty types would otherwise spend the
// consumer's budget on the vocabulary of the package rather than the change.
const typesPerDecl = 8

// signatureNamed collects the named types a signature mentions, following the
// type constructors that wrap them. It stops at a named type rather than
// descending into it, so a struct field's type is not pulled in.
func signatureNamed(sig *types.Signature) []*types.Named {
	out := namedIn(sig.Params())
	out = append(out, namedIn(sig.Results())...)
	if recv := sig.Recv(); recv != nil {
		out = append(out, namedIn(recv.Type())...)
	}
	return out
}

// namedIn collects the named types a type mentions, following the constructors
// that wrap them. It stops at a named type rather than descending into it, so a
// struct field's own field types are not pulled in with it.
func namedIn(t0 types.Type) []*types.Named {
	var out []*types.Named
	seen := map[*types.Named]bool{}
	var walk func(t types.Type, depth int)
	walk = func(t types.Type, depth int) {
		if t == nil || depth > 6 {
			return
		}
		if named, ok := t.(*types.Named); ok {
			if !seen[named] {
				seen[named] = true
				out = append(out, named)
			}
			return
		}
		for _, inner := range componentTypes(t) {
			walk(inner, depth+1)
		}
	}
	walk(t0, 0)
	return out
}

// componentTypes returns the types a constructor is built out of: a pointer's
// element, a map's key and value, a struct's field types, and so on. Named
// types are not one of them -- the walk above stops there, which is what keeps
// a field's own fields from being pulled in with it.
func componentTypes(t types.Type) []types.Type {
	switch v := t.(type) {
	case *types.Pointer:
		return []types.Type{v.Elem()}
	case *types.Slice:
		return []types.Type{v.Elem()}
	case *types.Array:
		return []types.Type{v.Elem()}
	case *types.Chan:
		return []types.Type{v.Elem()}
	case *types.Map:
		return []types.Type{v.Key(), v.Elem()}
	case *types.Signature:
		return []types.Type{v.Params(), v.Results()}
	case *types.Tuple:
		out := make([]types.Type, 0, v.Len())
		for x := range v.Variables() {
			out = append(out, x.Type())
		}
		return out
	case *types.Struct:
		var out []types.Type
		for f := range v.Fields() {
			out = append(out, f.Type())
		}
		return out
	case *types.Interface:
		var out []types.Type
		for m := range v.Methods() {
			out = append(out, m.Type())
		}
		return out
	}
	return nil
}

// declFor locates the declaration that defines an object. Positions come from
// the loader's shared file set, which is what lets an object found in one
// package variant point at a file another variant parsed.
func (b *builder) declFor(obj types.Object) *decl {
	if obj == nil || b.idx == nil || b.idx.fset == nil {
		return nil
	}
	pos := b.idx.fset.Position(obj.Pos())
	rel, ok := b.rel(pos.Filename)
	if !ok {
		return nil
	}
	return b.enclosingAt(rel, pos.Line)
}

// isChanged reports whether a declaration is one the diff touched.
//
// Declarations come from one per-file cache, so the same declaration is the
// same pointer. Comparing names as well would also match a different
// declaration that shares one, as every init function in a file does.
func (b *builder) isChanged(d *decl) bool {
	for _, c := range b.decls {
		if c == d {
			return true
		}
	}
	return false
}
