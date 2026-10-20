package gitfs

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "data"), []byte("0123456789"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "empty"), nil, 0600))
	for _, mapped := range []bool{false, true} {
		fs := New(root)
		fs.mmap = mapped
		f, err := fs.Open("data")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, f.Close()) })
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for off := range int64(10) {
					var b [1]byte
					n, err := f.ReadAt(b[:], off)
					assert.NoError(t, err)
					assert.Equal(t, 1, n)
					assert.Equal(t, byte('0'+off), b[0])
				}
			})
		}
		wg.Wait()
		_, err = f.Seek(3, io.SeekStart)
		require.NoError(t, err)
		data, err := io.ReadAll(f)
		require.NoError(t, err)
		require.Equal(t, "3456789", string(data))
		_, err = f.Write([]byte("x"))
		require.ErrorIs(t, err, os.ErrPermission)
		require.NoError(t, f.Close())
		_, err = f.ReadAt(make([]byte, 1), 0)
		require.Error(t, err, "closed mappings must not access unmapped memory")
		empty, err := fs.Open("empty")
		require.NoError(t, err)
		data, err = io.ReadAll(empty)
		require.NoError(t, err)
		require.Empty(t, data)
		require.NoError(t, empty.Close())
		_, err = fs.OpenFile("data", os.O_RDWR, 0600)
		require.ErrorIs(t, err, os.ErrPermission)
	}
}

func TestConfinement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	require.NoError(t, os.Mkdir(root, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "outside"), []byte("outside"), 0600))
	fs := New(root, WithMmap())
	_, err := fs.Open("../outside")
	require.Error(t, err)
	_, err = fs.Chroot("..")
	require.Error(t, err)
	_, err = fs.Open(".")
	require.Error(t, err)
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require privileges on Windows")
	}
	require.NoError(t, os.Symlink("../outside", filepath.Join(root, "escape")))
	_, err = fs.Open("escape")
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "inside"), []byte("inside"), 0600))
	require.NoError(t, os.Symlink("inside", filepath.Join(root, "link")))
	f, err := fs.Open("link")
	require.NoError(t, err)
	defer f.Close()
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "inside", string(data))
	entries, err := fs.ReadDir(".")
	require.NoError(t, err)
	require.Len(t, entries, 3)
}
