package gitobj

import (
	"fmt"
	"io"
)

type Reference struct {
	name   ReferenceName
	target ReferenceName
	id     Hash
	kind   ReferenceType
}

func NewHashReference(name ReferenceName, id Hash) *Reference {
	return &Reference{name: name, id: id, kind: HashReference, target: ""}
}
func NewSymbolicReference(name, target ReferenceName) *Reference {
	return &Reference{name: name, target: target, kind: SymbolicReference, id: ZeroHash}
}
func (r *Reference) Name() ReferenceName   { return r.name }
func (r *Reference) Target() ReferenceName { return r.target }
func (r *Reference) Hash() Hash            { return r.id }
func (r *Reference) Type() ReferenceType   { return r.kind }

type ReferenceIter struct {
	refs []*Reference
	pos  int
}

func NewReferenceIter(refs []*Reference) *ReferenceIter { return &ReferenceIter{refs: refs, pos: 0} }
func (*ReferenceIter) Close()                           {}
func (i *ReferenceIter) Next() (*Reference, error) {
	if i.pos == len(i.refs) {
		return nil, io.EOF
	}
	r := i.refs[i.pos]
	i.pos++
	return r, nil
}
func ResolveReference(s interface {
	Reference(name ReferenceName) (*Reference, error)
}, name ReferenceName) (*Reference, error) {
	for range 64 {
		r, err := s.Reference(name)
		if err != nil {
			return nil, err
		}
		if r.Type() != SymbolicReference {
			return r, nil
		}
		name = r.Target()
	}
	return nil, fmt.Errorf("symbolic reference cycle at %s", name)
}
