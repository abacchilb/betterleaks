package gogit

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/betterleaks/betterleaks/v2/internal/gitobj"
	"github.com/go-git/go-git/v5/config"
)

type repositoryStorage interface {
	gitobj.ObjectReader
	Reference(name gitobj.ReferenceName) (*gitobj.Reference, error)
	IterReferences() (*gitobj.ReferenceIter, error)
	HashesWithPrefix(prefix []byte) ([]gitobj.Hash, error)
	Config() (*config.Config, error)
}
type repository struct{ Storer repositoryStorage }

func (r *repository) Config() (*config.Config, error) { return r.Storer.Config() }
func (r *repository) Head() (*gitobj.Reference, error) {
	return gitobj.ResolveReference(r.Storer, gitobj.HEAD)
}
func (r *repository) CommitObject(id gitobj.Hash) (*gitobj.Commit, error) {
	o, err := r.Storer.EncodedObject(gitobj.CommitObject, id)
	if err != nil {
		return nil, err
	}
	return gitobj.DecodeCommit(r.Storer, o)
}
func (r *repository) TreeObject(id gitobj.Hash) (*gitobj.Tree, error) {
	o, err := r.Storer.EncodedObject(gitobj.TreeObject, id)
	if err != nil {
		return nil, err
	}
	return gitobj.DecodeTree(r.Storer, o)
}
func (r *repository) Object(kind gitobj.ObjectType, id gitobj.Hash) (any, error) {
	o, err := r.Storer.EncodedObject(kind, id)
	if err != nil {
		return nil, err
	}
	switch o.Type() {
	case gitobj.CommitObject:
		return gitobj.DecodeCommit(r.Storer, o)
	case gitobj.TreeObject:
		return gitobj.DecodeTree(r.Storer, o)
	case gitobj.TagObject:
		return gitobj.DecodeTag(r.Storer, o)
	case gitobj.BlobObject:
		return &gitobj.Blob{Hash: id}, nil
	}
	return nil, gitobj.ErrObjectNotFound
}
func (r *repository) Tags() (*gitobj.ReferenceIter, error) {
	iter, err := r.Storer.IterReferences()
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var refs []*gitobj.Reference
	for {
		ref, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if ref.Name().IsTag() {
			refs = append(refs, ref)
		}
	}
	return gitobj.NewReferenceIter(refs), nil
}
func (r *repository) peelCommit(id gitobj.Hash) (*gitobj.Commit, error) {
	for range maxTagNesting {
		o, err := r.Storer.EncodedObject(gitobj.AnyObject, id)
		if err != nil {
			return nil, err
		}
		switch o.Type() {
		case gitobj.CommitObject:
			return gitobj.DecodeCommit(r.Storer, o)
		case gitobj.TagObject:
			tag, err := gitobj.DecodeTag(r.Storer, o)
			if err != nil {
				return nil, err
			}
			id = tag.Target
		default:
			return nil, gitobj.ErrObjectNotFound
		}
	}
	return nil, fmt.Errorf("annotated tag nesting exceeds %d levels", maxTagNesting)
}
func (r *repository) revisionBase(name string) (*gitobj.Commit, error) {
	var ids []gitobj.Hash
	cfg, err := r.Config()
	if err != nil {
		return nil, err
	}
	size := gitobj.FromObjectFormat(cfg.Extensions.ObjectFormat).Size()
	if len(name) == size*2 && gitobj.IsHash(name) {
		ids = append(ids, gitobj.NewHash(name))
	} else if len(name) > 0 && len(name) < size*2 {
		// Validate the dangling nibble too. Truncating it before decoding
		// treats names like z or abz as possible object IDs, causing needless
		// enumeration and surfacing unrelated object-directory errors.
		hexName := name
		if len(name)%2 != 0 {
			hexName += "0"
		}
		if prefix, err := hex.DecodeString(hexName); err == nil {
			prefix = prefix[:len(name)/2]
			candidates, err := r.Storer.HashesWithPrefix(prefix)
			if err != nil {
				return nil, err
			}
			gitobj.HashesSort(candidates)
			for _, id := range candidates {
				if strings.HasPrefix(id.String(), strings.ToLower(name)) {
					ids = append(ids, id)
				}
			}
		}
	}
	// Object IDs take precedence over equally named refs. Resolve them before
	// reading refs so a broken same-named branch cannot invalidate a valid ID.
	for _, id := range ids {
		c, err := r.peelCommit(id)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, gitobj.ErrObjectNotFound) {
			return nil, err
		}
	}
	for _, pattern := range []string{"%s", "refs/%s", "refs/tags/%s", "refs/heads/%s", "refs/remotes/%s", "refs/remotes/%s/HEAD"} {
		ref, err := gitobj.ResolveReference(r.Storer, gitobj.ReferenceName(fmt.Sprintf(pattern, name)))
		if err == nil {
			c, err := r.peelCommit(ref.Hash())
			if errors.Is(err, gitobj.ErrObjectNotFound) {
				return nil, gitobj.ErrReferenceNotFound
			}
			return c, err
		}
		if !errors.Is(err, gitobj.ErrReferenceNotFound) {
			return nil, err
		}
	}
	return nil, gitobj.ErrReferenceNotFound
}

func (r *repository) ResolveRevision(rev gitobj.Revision) (*gitobj.Hash, error) {
	text := string(rev)
	cut := strings.IndexAny(text, "^~")
	if cut < 0 {
		cut = len(text)
	}
	base := text[:cut]
	if base == "@" {
		base = "HEAD"
	}
	if base == "" {
		return nil, gitobj.ErrReferenceNotFound
	}
	c, err := r.revisionBase(base)
	if err != nil {
		return nil, err
	}
	for tail := text[cut:]; tail != ""; {
		op := tail[0]
		tail = tail[1:]
		if op != '^' && op != '~' {
			return nil, errors.New("unsupported Git revision suffix")
		}
		if op == '^' && strings.HasPrefix(tail, "{") {
			end := strings.IndexByte(tail, '}')
			if end < 0 {
				return nil, errors.New("unterminated Git revision suffix")
			}
			selector := tail[1:end]
			tail = tail[end+1:]
			if selector == "" || selector == "commit" {
				continue
			}
			if !strings.HasPrefix(selector, "/") {
				return nil, errors.New("revision must resolve to a commit")
			}
			pattern := selector[1:]
			negate := strings.HasPrefix(pattern, "!-")
			if negate {
				pattern = pattern[2:]
			} else if strings.HasPrefix(pattern, "!!") {
				pattern = pattern[1:]
			} else if strings.HasPrefix(pattern, "!") {
				return nil, errors.New("unsupported Git message search modifier")
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, err
			}
			pending := []gitobj.Hash{c.Hash}
			seen := make(map[gitobj.Hash]bool)
			found := false
			for len(pending) > 0 {
				id := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				if seen[id] {
					continue
				}
				seen[id] = true
				candidate, err := r.CommitObject(id)
				if err != nil {
					return nil, err
				}
				if re.MatchString(candidate.Message) != negate {
					c, found = candidate, true
					break
				}
				for j := len(candidate.ParentHashes) - 1; j >= 0; j-- {
					pending = append(pending, candidate.ParentHashes[j])
				}
			}
			if !found {
				return nil, fmt.Errorf("no commit message matches %q", pattern)
			}
			continue
		}
		n := 0
		for n < len(tail) && tail[n] >= '0' && tail[n] <= '9' {
			n++
		}
		depth := 1
		if n > 0 {
			depth, err = strconv.Atoi(tail[:n])
			if err != nil {
				return nil, err
			}
		}
		tail = tail[n:]
		if op == '^' {
			if depth == 0 {
				continue
			}
			if depth > len(c.ParentHashes) {
				return nil, io.EOF
			}
			c, err = r.CommitObject(c.ParentHashes[depth-1])
			if err != nil {
				return nil, err
			}
		} else {
			for range depth {
				if len(c.ParentHashes) == 0 {
					return nil, io.EOF
				}
				c, err = r.CommitObject(c.ParentHashes[0])
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return &c.Hash, nil
}
