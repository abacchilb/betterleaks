package gitobj

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testIndexFile struct{ *bytes.Reader }

func (testIndexFile) Close() error     { return nil }
func indexFile(b []byte) testIndexFile { return testIndexFile{bytes.NewReader(b)} }

func fixtureIndex(format ObjectFormat) ([]byte, []byte, Hash, []Hash, []int64) {
	var idx, rev []byte
	var ids []Hash
	pack := NewHash(strings.Repeat("c", FromObjectFormat(format).Size()*2))
	for _, prefix := range []string{"01", "01", "80", "ff"} {
		id := pack
		id.data[0] = NewHash(prefix + strings.Repeat("0", pack.Size()*2-2)).data[0]
		id.data[1] = byte(len(ids))
		ids = append(ids, id)
	}
	offsets := []int64{100, 12, 1 << 33, 55}
	idx = []byte{255, 't', 'O', 'c', 0, 0, 0, 2}
	for b := range 256 {
		n := uint32(0)
		for _, id := range ids {
			if int(id.data[0]) <= b {
				n++
			}
		}
		idx = binary.BigEndian.AppendUint32(idx, n)
	}
	for _, id := range ids {
		idx = append(idx, id.Bytes()...)
	}
	for range ids {
		idx = binary.BigEndian.AppendUint32(idx, 0)
	}
	for _, off := range offsets {
		if off > 0x7fffffff {
			idx = binary.BigEndian.AppendUint32(idx, 0x80000000)
		} else {
			idx = binary.BigEndian.AppendUint32(idx, uint32(off))
		}
	}
	idx = binary.BigEndian.AppendUint64(idx, 1<<33)
	idx = append(idx, pack.Bytes()...)
	idx = append(idx, make([]byte, pack.Size())...)
	algo := uint32(1)
	if format == SHA256 {
		algo = 2
	}
	rev = []byte{'R', 'I', 'D', 'X', 0, 0, 0, 1}
	rev = binary.BigEndian.AppendUint32(rev, algo)
	for _, n := range []uint32{1, 3, 0, 2} {
		rev = binary.BigEndian.AppendUint32(rev, n)
	}
	rev = append(rev, pack.Bytes()...)
	rev = append(rev, make([]byte, pack.Size())...)
	return idx, rev, pack, ids, offsets
}

func TestLazyIndex(t *testing.T) {
	for _, format := range []ObjectFormat{SHA1, SHA256} {
		for _, reverse := range []bool{false, true} {
			idx, rev, pack, ids, offsets := fixtureIndex(format)
			var reverseFile ReadAtCloser
			if reverse {
				reverseFile = indexFile(rev)
			}
			i, err := OpenIndex(t.Context(), indexFile(idx), reverseFile, pack)
			require.NoError(t, err)
			defer i.Close()
			require.Nil(t, i.order, "opening an index must not allocate per-object tables")
			for n, id := range ids {
				off, err := i.FindOffset(id)
				require.NoError(t, err)
				require.Equal(t, offsets[n], off)
			}
			require.Nil(t, i.order, "forward lookups must remain lazy even without .rev")
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					for n, off := range offsets {
						id, err := i.FindHash(off)
						assert.NoError(t, err)
						assert.Equal(t, ids[n], id)
					}
				})
			}
			wg.Wait()
			if reverse {
				require.Nil(t, i.order)
			} else {
				require.Len(t, i.order, len(ids))
			}
			got, err := i.HashesWithPrefix([]byte{1})
			require.NoError(t, err)
			require.Equal(t, ids[:2], got)
			_, err = i.FindHash(77)
			require.ErrorIs(t, err, ErrObjectNotFound)
			_, err = i.FindOffset(NewHash(strings.Repeat("f", pack.Size()*2)))
			require.ErrorIs(t, err, ErrObjectNotFound)
			iter, err := i.Entries()
			require.NoError(t, err)
			for n := range ids {
				entry, err := iter.Next()
				require.NoError(t, err)
				require.Equal(t, ids[n], entry.Hash)
				require.Equal(t, uint64(offsets[n]), entry.Offset)
			}
			_, err = iter.Next()
			require.ErrorIs(t, err, io.EOF)
		}
	}
}

func TestIndexCorruption(t *testing.T) {
	idx, rev, pack, _, _ := fixtureIndex(SHA1)
	for _, target := range []string{"version", "fanout", "count", "truncated", "pack-checksum", "reverse-version", "reverse-width", "reverse-count", "reverse-checksum"} {
		t.Run(target, func(t *testing.T) {
			a, b := bytes.Clone(idx), bytes.Clone(rev)
			switch target {
			case "version":
				a[7] = 1
			case "fanout":
				binary.BigEndian.PutUint32(a[8:12], 3)
			case "count":
				binary.BigEndian.PutUint32(a[1028:1032], 0xffffffff)
			case "truncated":
				a = a[:len(a)-1]
			case "pack-checksum":
				a[len(a)-40] ^= 1
			case "reverse-version":
				b[7] = 2
			case "reverse-width":
				b[11] = 2
			case "reverse-count":
				b = b[:len(b)-1]
			case "reverse-checksum":
				b[len(b)-40] ^= 1
			}
			_, err := OpenIndex(t.Context(), indexFile(a), indexFile(b), pack)
			require.Error(t, err)
		})
	}
}

func FuzzIndex(f *testing.F) {
	a, b, _, _, _ := fixtureIndex(SHA1)
	f.Add(a, b)
	f.Add(a, []byte(nil))
	f.Fuzz(func(t *testing.T, idx, rev []byte) {
		if len(idx)+len(rev) > 65536 {
			t.Skip()
		}
		pack := NewHash(strings.Repeat("c", 40))
		var reverseFile ReadAtCloser
		if len(rev) > 0 {
			reverseFile = indexFile(rev)
		}
		i, err := OpenIndex(t.Context(), indexFile(idx), reverseFile, pack)
		if err != nil {
			return
		}
		defer i.Close()
		_, _ = i.FindOffset(pack)
		_, _ = i.FindHash(12)
		_, _ = i.HashesWithPrefix([]byte{0})
	})
}

func TestIndexCancellation(t *testing.T) {
	idx, _, pack, _, offsets := fixtureIndex(SHA1)
	ctx, cancel := context.WithCancel(t.Context())
	i, err := OpenIndex(ctx, indexFile(idx), nil, pack)
	require.NoError(t, err)
	defer i.Close()
	cancel()
	_, err = i.FindHash(offsets[0])
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, i.order)
	_, err = i.HashesWithPrefix([]byte{1})
	require.ErrorIs(t, err, context.Canceled)
}
