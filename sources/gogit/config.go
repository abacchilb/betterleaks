package gogit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/betterleaks/betterleaks/v2/internal/gitobj"
	"github.com/go-git/gcfg/v2"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

// parseRepositoryConfig projects the settings used by the read-only source
// onto v5's config types. Its general-purpose decoder has quadratic section
// lookup and incompatible branch/URL semantics, so use stable gcfg's syntax
// parser with indexed section lookup. This configuration is never written.
func parseRepositoryConfig(raw string) (*config.Config, error) {
	c := config.NewConfig()
	decoded, err := decodeRepositoryConfig(raw)
	if err != nil {
		return nil, err
	}
	c.Raw = decoded
	// Retain main's validation of settings outside the scanner's projection.
	if window := decoded.Section("pack").Option("window"); window != "" {
		if _, err := strconv.ParseUint(window, 10, 32); err != nil {
			return nil, err
		}
	}
	if version := decoded.Section("protocol").Option("version"); version != "" && version != "0" && version != "1" && version != "2" {
		return nil, fmt.Errorf("unsupported Git protocol version %q", version)
	}
	for _, branch := range decoded.Section("branch").Subsections {
		b := &config.Branch{
			Name: branch.Name, Remote: branch.Options.Get("remote"),
			Merge:       plumbing.ReferenceName(branch.Options.Get("merge")),
			Rebase:      branch.Options.Get("rebase"),
			Description: strings.ReplaceAll(branch.Options.Get("description"), `\n`, "\n"),
		}
		if err := gitobj.ValidateReferenceName(plumbing.NewBranchReferenceName(b.Name)); err != nil {
			return nil, err
		}
		validation := *b
		validation.Name, validation.Merge = "branch", ""
		if err := validation.Validate(); err != nil {
			return nil, err
		}
		c.Branches[b.Name] = b
	}
	for _, remote := range decoded.Section("remote").Subsections {
		r := &config.RemoteConfig{Name: remote.Name, URLs: append(remote.Options.GetAll("url"), remote.Options.GetAll("pushurl")...), Mirror: remote.Options.Get("mirror") == "true", Fetch: nil}
		for _, value := range remote.Options.GetAll("fetch") {
			spec := config.RefSpec(value)
			if err := spec.Validate(); err != nil {
				return nil, err
			}
			r.Fetch = append(r.Fetch, spec)
		}
		c.Remotes[r.Name] = r
	}
	applyRepositoryURLRules(c)
	return c, nil
}

func decodeRepositoryConfig(raw string) (*format.Config, error) {
	c := format.New()
	sections := make(map[string]*format.Section)
	subsections := make(map[*format.Section]map[string]*format.Subsection)
	var lastName, lastSub string
	var section *format.Section
	var subsection *format.Subsection
	err := gcfg.ReadWithCallback(strings.NewReader(raw), func(name, sub, key, value string, _ bool) error {
		// Repeated options in a long-named section must not rehash its name.
		if section == nil || name != lastName || sub != lastSub {
			folded := strings.ToLower(name)
			section = sections[folded]
			if section == nil {
				section = &format.Section{Name: name, Options: nil, Subsections: nil}
				sections[folded] = section
				c.Sections = append(c.Sections, section)
			}
			subsection = nil
			if sub != "" {
				byName := subsections[section]
				if byName == nil {
					byName = make(map[string]*format.Subsection)
					subsections[section] = byName
				}
				subsection = byName[sub]
				if subsection == nil {
					subsection = &format.Subsection{Name: sub, Options: nil}
					byName[sub] = subsection
					section.Subsections = append(section.Subsections, subsection)
				}
			}
			lastName, lastSub = name, sub
		}
		if key != "" {
			option := &format.Option{Key: key, Value: value}
			if subsection != nil {
				subsection.Options = append(subsection.Options, option)
			} else {
				section.Options = append(section.Options, option)
			}
		}
		return nil
	})
	return c, err
}

// Worktree config accepts Git's textual and numeric boolean spellings. In
// particular, v6 loads overlays enabled by 1, yes, or on even though its
// extension checker rejects those spellings when no overlay file exists.
func parseConfigBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	}
	n, err := strconv.Atoi(value)
	return n != 0, err
}

// v5 keeps only the last insteadOf value and chooses equal-length matches in
// map iteration order. Recompute from the raw remote URLs with Git/v6's
// multiple-value, longest-match, first-tie semantics. Empty prefixes and
// pushInsteadOf do not rewrite fetch URLs. This config is never written back.
func applyRepositoryURLRules(c *config.Config) {
	if !c.Raw.HasSection("url") {
		return
	}
	rules := c.Raw.Section("url").Subsections
	for _, remote := range c.Remotes {
		for n, original := range remote.URLs {
			longest := 0
			for _, rule := range rules {
				for _, prefix := range rule.Options.GetAll("insteadOf") {
					if len(prefix) > longest && strings.HasPrefix(original, prefix) {
						longest = len(prefix)
						remote.URLs[n] = rule.Name + original[longest:]
					}
				}
			}
		}
	}
}
