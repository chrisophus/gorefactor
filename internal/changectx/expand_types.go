package changectx

import (
	"fmt"
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
		sig, ok := d.obj.Type().(*types.Signature)
		if !ok {
			continue
		}
		for _, named := range signatureNamed(sig) {
			obj := named.Obj()
			if obj == nil || obj.Pkg() == nil || seen[obj.Pos()] {
				continue
			}
			seen[obj.Pos()] = true
			target := b.declFor(obj)
			if target == nil || b.isChanged(target) {
				continue // its own declaration already carries the change
			}
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
				},
			})
		}
	}
}

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
	for _, ct := range changed {
		for _, iface := range ifaces {
			it, ok := iface.named.Underlying().(*types.Interface)
			if !ok || it.NumMethods() == 0 || !satisfies(ct.named, it) {
				continue
			}
			b.addSiblings(ct, iface, it, concrete, emitted)
		}
	}
}

// namedType pairs a type with the declaration that defines it, so an
// expansion can report a source span without looking the position up twice.
type namedType struct {
	named *types.Named
	decl  *decl
}

func (b *builder) addSiblings(ct, iface namedType, it *types.Interface, concrete []namedType, emitted map[string]bool) {
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

// signatureNamed collects the named types a signature mentions, following the
// type constructors that wrap them. It stops at a named type rather than
// descending into it, so a struct field's type is not pulled in.
func signatureNamed(sig *types.Signature) []*types.Named {
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
		}
	}
	walk(sig.Params(), 0)
	walk(sig.Results(), 0)
	if recv := sig.Recv(); recv != nil {
		walk(recv.Type(), 0)
	}
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
func (b *builder) isChanged(d *decl) bool {
	for _, c := range b.decls {
		if c == d || (c.rel == d.rel && c.symbol == d.symbol) {
			return true
		}
	}
	return false
}
