//nolint:exhaustruct // Each change represents an addition or deletion.
package gitobj

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/stretchr/testify/require"
)

type testObjectStore map[Hash]EncodedObject

func (s testObjectStore) EncodedObject(_ ObjectType, id Hash) (EncodedObject, error) {
	if o := s[id]; o != nil {
		return o, nil
	}
	return nil, ErrObjectNotFound
}

func TestRenamesPreserveFullSHA256IDs(t *testing.T) {
	// Two unrelated IDs have the same SHA-1-sized prefix. Truncating them
	// would manufacture a false exact rename and lose the new file's content.
	a := NewHash(strings.Repeat("a", 40) + strings.Repeat("1", 24))
	b := NewHash(strings.Repeat("a", 40) + strings.Repeat("2", 24))
	store := testObjectStore{
		a: encoded(t, SHA256, BlobObject, strings.Repeat("old only\n", 10)),
		b: encoded(t, SHA256, BlobObject, strings.Repeat("new only\n", 10)),
	}
	tree := &Tree{store: store}
	entry := func(name string, id Hash) ChangeEntry {
		return ChangeEntry{Name: name, Tree: tree, TreeEntry: TreeEntry{Name: name, Mode: filemode.Regular, Hash: id}}
	}
	options := &DiffTreeOptions{DetectRenames: true, RenameScore: 50, RenameLimit: 1000}
	changes := Changes{&Change{From: entry("old", a)}, &Change{To: entry("new", b)}}
	got, err := DetectRenames(changes, options)
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, c := range got {
		require.True(t, c.From.Name == "" || c.To.Name == "")
	}
	changes[1].To.TreeEntry.Hash = a
	got, err = DetectRenames(changes, options)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "old", got[0].From.Name)
	require.Equal(t, "new", got[0].To.Name)
	require.Equal(t, "new", got[0].To.TreeEntry.Name)
	require.Equal(t, a, got[0].To.TreeEntry.Hash)
}
