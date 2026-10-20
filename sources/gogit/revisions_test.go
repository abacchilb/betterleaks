//nolint:exhaustruct // Fixtures specify only the fields relevant to the scenario.
package gogit

import (
	"strings"
	"testing"

	"github.com/betterleaks/betterleaks/v2/internal/gitobj"
	"github.com/stretchr/testify/require"
)

func TestResolveRevisionAgainstGit(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			gitFixture(t, dir, "init", "-q", "-b", "main", "--object-format="+format)
			gitFixture(t, dir, "config", "user.name", "Test")
			gitFixture(t, dir, "config", "user.email", "test@example.test")
			commitFile(t, dir, "a", "one\n", "initial")
			commitFile(t, dir, "a", "two\n", "!important fix")
			commitFile(t, dir, "a", "three\n", "last")
			gitFixture(t, dir, "tag", "-am", "release", "release")
			gitFixture(t, dir, "tag", "light")
			gitFixture(t, dir, "branch", "üñicode")
			gitFixture(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
			gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
			gitFixture(t, dir, "pack-refs", "--all")
			gitFixture(t, dir, "repack", "-adq")
			id := gitFixture(t, dir, "rev-parse", "HEAD")
			r, close, err := (&Source{RepoPath: dir}).open(t.Context())
			require.NoError(t, err)
			defer close()
			for _, rev := range []string{strings.ToUpper(id), strings.ToUpper(id[:8]), strings.ToUpper(id[:9])} {
				got, err := r.ResolveRevision(gitobj.Revision(rev))
				require.NoError(t, err)
				require.Equal(t, id, got.String())
			}
			for _, rev := range []string{"HEAD", "@", "main", "heads/main", "refs/heads/main", "release", "release^{}", "release^{commit}", "light", "üñicode", "origin", "origin/main", "HEAD^", "HEAD^^", "HEAD~", "HEAD~2", "HEAD~^", "HEAD^0", "HEAD~0", "HEAD^{/initial}", "HEAD^{/!-last}", "HEAD^{/!!important}", id, id[:8], id[:9]} {
				t.Run(rev, func(t *testing.T) {
					want := gitFixture(t, dir, "rev-parse", rev+"^{commit}")
					got, err := r.ResolveRevision(gitobj.Revision(rev))
					require.NoError(t, err)
					require.Equal(t, want, got.String())
				})
			}
			for _, rev := range []string{"", "missing", "HEAD^2", "HEAD~3", "HEAD^{/missing}", "HEAD^{/!reserved}", "HEAD^{/[}", "HEAD^{", "HEAD~x", "HEAD^999999999999999999999"} {
				_, err := r.ResolveRevision(gitobj.Revision(rev))
				require.Error(t, err, rev)
			}
		})
	}
}
