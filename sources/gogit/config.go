package gogit

import (
	"bytes"
	"strings"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

// parseRepositoryConfig preserves v6's acceptance of arbitrary branch.merge
// values. v5 incorrectly requires refs/heads/, rejecting repositories with
// PR-tracking branches (refs/pull/123/head), for example. On that path, omit
// merge options while v5 builds the typed config, then restore them in memory.
// Keep the ordinary path unchanged and retain all other config validation.
func parseRepositoryConfig(raw string) (*config.Config, error) {
	c := config.NewConfig()
	parseErr := c.Unmarshal([]byte(raw))
	if parseErr == nil {
		return c, nil
	}

	// Decode again to verify the entire syntax: Unmarshal can leave a partially
	// parsed Raw on failure, which must never be treated as a complete config.
	parsed := format.New()
	if err := format.NewDecoder(strings.NewReader(raw)).Decode(parsed); err != nil {
		return nil, err
	}
	saved := make(map[string]format.Options)
	for _, branch := range parsed.Section("branch").Subsections {
		merge := plumbing.ReferenceName(branch.Options.Get("merge"))
		if merge == "" || merge.IsBranch() {
			continue
		}
		saved[branch.Name] = branch.Options
		branch.RemoveOption("merge")
	}
	if len(saved) == 0 {
		return nil, parseErr
	}
	var encoded bytes.Buffer
	if err := format.NewEncoder(&encoded).Encode(parsed); err != nil {
		return nil, err
	}
	c = config.NewConfig()
	if err := c.Unmarshal(encoded.Bytes()); err != nil {
		return nil, err
	}
	for name, options := range saved {
		c.Raw.Section("branch").Subsection(name).Options = options
		c.Branches[name].Merge = plumbing.ReferenceName(options.Get("merge"))
	}
	return c, nil
}
