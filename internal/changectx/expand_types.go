package changectx

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
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

// expandSiblings emits the other implementations of an interface a changed
// type satisfies. It answers the question a diff cannot: whether the same
// change is owed to the types that sit beside this one.
func (b *builder) expandSiblings() {
	changed := b.changedNamedTypes()
	if len(changed) == 0 || b.idx == nil {
		return
	}
	ifaces, concrete := b.moduleTypes()
	emitted := map[string]bool{}
	ifaceSeen := map[string]bool{}
	// When the interface itself is what changed, the types implementing it are
	// the answer: they are the ones owed the same change. The walk below starts
	// from changed concrete types and never reached this.
	b.addImplementations(concrete, emitted)
	for _, ct := range changed {
		for _, iface := range ifaces {
			it, ok := iface.named.Underlying().(*types.Interface)
			if !ok || it.NumMethods() == 0 || !satisfies(ct.named, it) {
				continue
			}
			if b.addSiblings(ct, iface, it, concrete, emitted) > 0 {
				b.addInterface(iface, ifaceSeen)
			}
		}
	}
}

// addImplementations emits the types that implement an interface the change
// edits. A changed interface is a changed contract, and the reviewer's question
// is which implementations still keep it, which is the sibling question asked
// from the other direction.
func (b *builder) addImplementations(concrete []namedType, emitted map[string]bool) {
	for _, d := range b.decls {
		if d.kind != "type" || d.obj == nil {
			continue
		}
		named, ok := d.obj.Type().(*types.Named)
		if !ok {
			continue
		}
		it, ok := named.Underlying().(*types.Interface)
		if !ok || it.NumMethods() == 0 {
			continue
		}
		kept, skipped := 0, 0
		for _, other := range concrete {
			if other.decl == nil || b.isChanged(other.decl) || !implements(other.named, it) {
				continue
			}
			key := d.scope + "|" + other.decl.scope
			if emitted[key] {
				continue
			}
			if kept >= siblingsPerInterface {
				skipped++
				continue
			}
			emitted[key] = true
			kept++
			b.add(Expansion{
				Role:      RoleSibling,
				Priority:  priorityFor(other.decl),
				Symbol:    other.decl.symbol,
				Scope:     other.decl.scope,
				File:      other.decl.rel,
				StartLine: other.decl.start,
				EndLine:   other.decl.end,
				Content:   b.slice(other.decl.rel, other.decl.start, other.decl.end),
				Details: map[string]string{
					"kind":              "implementation",
					"interface":         d.scope,
					"implementsChanged": "true",
				},
			})
		}
		if skipped > 0 {
			b.notes = append(b.notes, fmt.Sprintf(
				"%d further implementation(s) of %s were not expanded (cap %d per interface)",
				skipped, d.scope, siblingsPerInterface))
		}
	}
}

// addInterface emits the interface a changed type implements, once, and only
// when a sibling was emitted under it.
//
// The sibling role names it in details.interface and has never sent it. With no
// sibling there is no details.interface either, so there is nothing to complete
// and the interface is one more type the reviewer did not ask for. A
// reviewer holding two implementations and no interface has been shown that
// the two are peers and not what they are peers under, which is the only place
// the contract they both have to keep is written down.
func (b *builder) addInterface(iface namedType, seen map[string]bool) {
	if iface.decl == nil || seen[iface.decl.scope] {
		return
	}
	seen[iface.decl.scope] = true
	if b.isChanged(iface.decl) {
		return // the enclosing role already carries it
	}
	b.add(Expansion{
		Role:      RoleType,
		Priority:  priorityFor(iface.decl),
		Symbol:    iface.decl.symbol,
		Scope:     iface.decl.scope,
		File:      iface.decl.rel,
		StartLine: iface.decl.start,
		EndLine:   iface.decl.end,
		Content:   b.slice(iface.decl.rel, iface.decl.start, iface.decl.end),
		Details: map[string]string{
			"kind":     "interface",
			"whyShown": "the interface the changed type implements",
		},
	})
}

// namedType pairs a type with the declaration that defines it, so an
// expansion can report a source span without looking the position up twice.
type namedType struct {
	named *types.Named
	decl  *decl
}

func (b *builder) addSiblings(ct, iface namedType, it *types.Interface, concrete []namedType, emitted map[string]bool) int {
	kept := 0
	skipped := 0
	for _, other := range concrete {
		if other.named == ct.named || other.decl == nil || !satisfies(other.named, it) {
			continue
		}
		key := ct.decl.scope + "|" + other.decl.scope
		if emitted[key] {
			continue
		}
		if kept >= siblingsPerInterface {
			skipped++
			continue
		}
		emitted[key] = true
		kept++
		b.add(Expansion{
			Role:      RoleSibling,
			Priority:  priorityFor(ct.decl),
			Symbol:    other.decl.symbol,
			Scope:     other.decl.scope,
			File:      other.decl.rel,
			StartLine: other.decl.start,
			EndLine:   other.decl.end,
			Content:   b.slice(other.decl.rel, other.decl.start, other.decl.end),
			Details: map[string]string{
				"kind":      "implementation",
				"interface": iface.decl.scope,
				"peerOf":    ct.decl.scope,
			},
		})
	}
	if skipped > 0 {
		b.notes = append(b.notes, fmt.Sprintf(
			"%d further implementation(s) of %s were not expanded (cap %d per interface)",
			skipped, iface.decl.scope, siblingsPerInterface))
	}
	return kept
}

// changedNamedTypes returns the named types the change touches: types declared
// in a changed declaration, and the receiver types of changed methods.
func (b *builder) changedNamedTypes() []namedType {
	seen := map[token.Pos]bool{}
	var out []namedType
	for _, d := range b.decls {
		var named *types.Named
		switch {
		case d.kind == "type" && d.obj != nil:
			named, _ = d.obj.Type().(*types.Named)
		case d.receiver != "" && d.unit != nil && d.unit.pkg != nil && d.unit.pkg.Types != nil:
			if obj := d.unit.pkg.Types.Scope().Lookup(d.receiver); obj != nil {
				named, _ = obj.Type().(*types.Named)
			}
		}
		if named == nil || named.Obj() == nil || seen[named.Obj().Pos()] {
			continue
		}
		target := b.declFor(named.Obj())
		if target == nil {
			continue
		}
		seen[named.Obj().Pos()] = true
		out = append(out, namedType{named: named, decl: target})
	}
	return out
}

// moduleTypes returns every named type declared in the loaded module, split
// into interfaces and the rest. Only module types are considered: matching
// against the standard library would report every type in the repository as a
// sibling under error or fmt.Stringer.
func (b *builder) moduleTypes() (ifaces, concrete []namedType) {
	if b.idx == nil {
		return nil, nil
	}
	seen := map[token.Pos]bool{}
	for _, p := range b.idx.pkgs {
		if p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		names := append([]string(nil), scope.Names()...)
		sort.Strings(names)
		for _, n := range names {
			tn, ok := scope.Lookup(n).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || seen[tn.Pos()] {
				continue
			}
			seen[tn.Pos()] = true
			d := b.declFor(tn)
			if d == nil {
				continue
			}
			entry := namedType{named: named, decl: d}
			if _, isIface := named.Underlying().(*types.Interface); isIface {
				ifaces = append(ifaces, entry)
			} else {
				concrete = append(concrete, entry)
			}
		}
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].decl.scope < ifaces[j].decl.scope })
	sort.Slice(concrete, func(i, j int) bool { return concrete[i].decl.scope < concrete[j].decl.scope })
	return ifaces, concrete
}

// satisfies reports whether a type or a pointer to it implements an interface.
// The pointer half matters because a method set with pointer receivers only
// satisfies the interface through the pointer.
func satisfies(t *types.Named, it *types.Interface) bool {
	return types.Implements(t, it) || types.Implements(types.NewPointer(t), it)
}

// implements reports whether a type is an implementation of an interface for
// the purpose of a changed contract, which is not the same question satisfies
// asks.
//
// An interface that gained a method is exactly the case where its
// implementations stop satisfying it, and that is the moment a reviewer most
// needs to see them: matching on satisfies alone would find the types that are
// still fine and hide every one the change broke. So a type counts when it
// satisfies the interface, or when it shares a method with it -- same name,
// identical signature -- which is what an implementation halfway through a
// contract change looks like.
//
// The shared method has to match by signature, not by name. A type with an
// unrelated Write is not an implementation of a Writer.
func implements(t *types.Named, it *types.Interface) bool {
	if satisfies(t, it) {
		return true
	}
	set := types.NewMethodSet(types.NewPointer(t))
	for i := range it.NumMethods() {
		m := it.Method(i)
		sel := set.Lookup(m.Pkg(), m.Name())
		if sel == nil {
			continue
		}
		if types.Identical(sel.Obj().Type(), m.Type()) {
			return true
		}
	}
	return false
}

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
		switch v := t.(type) {
		case *types.Named:
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		case *types.Pointer:
			walk(v.Elem(), depth+1)
		case *types.Slice:
			walk(v.Elem(), depth+1)
		case *types.Array:
			walk(v.Elem(), depth+1)
		case *types.Chan:
			walk(v.Elem(), depth+1)
		case *types.Map:
			walk(v.Key(), depth+1)
			walk(v.Elem(), depth+1)
		case *types.Signature:
			walk(v.Params(), depth+1)
			walk(v.Results(), depth+1)
		case *types.Tuple:
			for i := 0; i < v.Len(); i++ {
				walk(v.At(i).Type(), depth+1)
			}
		case *types.Struct:
			for i := range v.NumFields() {
				walk(v.Field(i).Type(), depth+1)
			}
		case *types.Interface:
			for i := range v.NumMethods() {
				walk(v.Method(i).Type(), depth+1)
			}
		}
	}
	walk(t0, 0)
	return out
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
