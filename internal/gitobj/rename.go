package gitobj

import (
	"encoding/binary"
	"path"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

type DiffTreeOptions = object.DiffTreeOptions

// DetectRenames uses v5's scoring with a scan-local, bijective ID table. These
// synthetic IDs never leave the detector or identify stored Git objects. In
// particular, SHA-256 IDs are never truncated into SHA-1 IDs.
func DetectRenames(changes Changes, opts *DiffTreeOptions) (Changes, error) {
	store := &renameStore{Storage: memory.NewStorage(), entries: make(map[plumbing.Hash]ChangeEntry)}
	ids := make(map[Hash]plumbing.Hash)
	empty := &plumbing.MemoryObject{}
	empty.SetType(plumbing.TreeObject)
	tree, err := object.DecodeTree(store, empty)
	if err != nil {
		return nil, err
	}
	convert := func(e ChangeEntry) object.ChangeEntry {
		if e.Name == "" {
			var empty object.ChangeEntry
			return empty
		}
		id, ok := ids[e.TreeEntry.Hash]
		if !ok {
			binary.BigEndian.PutUint64(id[:8], uint64(len(ids)+1))
			ids[e.TreeEntry.Hash] = id
			store.entries[id] = e
		}
		return object.ChangeEntry{Name: e.Name, Tree: tree, TreeEntry: object.TreeEntry{Name: "blob", Hash: id, Mode: e.TreeEntry.Mode}}
	}
	input := make(object.Changes, 0, len(changes))
	for _, c := range changes {
		input = append(input, &object.Change{From: convert(c.From), To: convert(c.To)})
	}
	paired, err := object.DetectRenames(input, opts)
	if err != nil {
		return nil, err
	}
	restore := func(e object.ChangeEntry) ChangeEntry {
		if e.Name == "" {
			var empty ChangeEntry
			return empty
		}
		original := store.entries[e.TreeEntry.Hash]
		original.Name = e.Name
		original.TreeEntry.Name = path.Base(e.Name)
		original.TreeEntry.Mode = e.TreeEntry.Mode
		return original
	}
	out := make(Changes, 0, len(paired))
	for _, c := range paired {
		out = append(out, &Change{From: restore(c.From), To: restore(c.To)})
	}
	return out, nil
}

type renameStore struct {
	*memory.Storage
	entries map[plumbing.Hash]ChangeEntry
}

func (s *renameStore) EncodedObject(kind plumbing.ObjectType, id plumbing.Hash) (plumbing.EncodedObject, error) {
	e, ok := s.entries[id]
	if !ok || e.Tree == nil {
		return nil, ErrObjectNotFound
	}
	o, err := e.Tree.store.EncodedObject(kind, e.TreeEntry.Hash)
	if err != nil {
		return nil, err
	}
	return v5Object{o, id}, nil
}

type v5Object struct {
	EncodedObject
	id plumbing.Hash
}

func (o v5Object) Hash() plumbing.Hash { return o.id }

type Blob struct{ Hash Hash }
