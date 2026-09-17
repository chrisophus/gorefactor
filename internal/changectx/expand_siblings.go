package changectx

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
)

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
		it := changedInterface(d)
		if it == nil {
			continue
		}
		if skipped := b.addImplementationsOf(d, it, concrete, emitted); skipped > 0 {
			b.notes = append(b.notes, fmt.Sprintf(
				"%d further implementation(s) of %s were not expanded (cap %d per interface)",
				skipped, d.scope, siblingsPerInterface))
		}
	}
}

// changedInterface returns the interface a changed declaration declares, or nil
// when it declares something else. An interface with no method is skipped for
// the reason the sibling walk skips one: nearly everything satisfies it.
func changedInterface(d *decl) *types.Interface {
	if d.kind != "type" || d.obj == nil {
		return nil
	}
	named, ok := d.obj.Type().(*types.Named)
	if !ok {
		return nil
	}
	it, ok := named.Underlying().(*types.Interface)
	if !ok || it.NumMethods() == 0 {
		return nil
	}
	return it
}

// addImplementationsOf emits the types implementing one changed interface, and
// reports how many the cap turned away.
func (b *builder) addImplementationsOf(d *decl, it *types.Interface, concrete []namedType, emitted map[string]bool) int {
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
	return skipped
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
			entry, ok := b.namedTypeIn(scope, n, seen)
			if !ok {
				continue
			}
			if _, isIface := entry.named.Underlying().(*types.Interface); isIface {
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

// namedTypeIn resolves one name in a package scope to the named type it
// declares and the declaration that writes it, or reports that it is not one:
// an alias, an unnamed type, a name already seen in another package variant, or
// a type declared where this provider cannot read it.
func (b *builder) namedTypeIn(scope *types.Scope, name string, seen map[token.Pos]bool) (namedType, bool) {
	tn, ok := scope.Lookup(name).(*types.TypeName)
	if !ok || tn.IsAlias() {
		return namedType{}, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok || seen[tn.Pos()] {
		return namedType{}, false
	}
	seen[tn.Pos()] = true
	d := b.declFor(tn)
	if d == nil {
		return namedType{}, false
	}
	return namedType{named: named, decl: d}, true
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
	for m := range it.Methods() {
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
