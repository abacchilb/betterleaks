package gogit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepositoryConfigMergeValues(t *testing.T) {
	raw := `[core]
	repositoryformatversion = 0
[url "https://github.com/"]
	insteadOf = gh:
[remote "review"]
	url = gh:example/review
	fetch = +refs/pull/*/head:refs/remotes/review/*
[branch "topic"]
	remote = review
	merge = refs/heads/main
	MeRgE = refs/pull/83/head
	rebase = interactive
	description = "review\nwith \"quotes\" and \\backslashes"
[branch "ordinary"]
	merge = refs/heads/main
[custom]
	value = "a # comment; tab\tand newline\n"
`
	cfg, err := parseRepositoryConfig(raw)
	require.NoError(t, err)
	branch := cfg.Branches["topic"]
	require.Equal(t, "review", branch.Remote)
	require.Equal(t, "refs/pull/83/head", branch.Merge.String())
	require.Equal(t, "interactive", branch.Rebase)
	require.Equal(t, "review\nwith \"quotes\" and \\backslashes", branch.Description)
	require.Equal(t, []string{"refs/heads/main", "refs/pull/83/head"}, cfg.Raw.Section("branch").Subsection("topic").Options.GetAll("merge"))
	require.Equal(t, "refs/heads/main", cfg.Branches["ordinary"].Merge.String())
	require.Equal(t, []string{"https://github.com/example/review"}, cfg.Remotes["review"].URLs)
	require.Equal(t, "+refs/pull/*/head:refs/remotes/review/*", cfg.Remotes["review"].Fetch[0].String())
	require.Equal(t, "a # comment; tab\tand newline\n", cfg.Raw.Section("custom").Option("value"))
}

func TestRepositoryConfigMergeDoesNotHideErrors(t *testing.T) {
	for name, extra := range map[string]string{
		"syntax":        "[unterminated\n",
		"branch name":   "[branch \"bad..name\"]\nremote = origin\n",
		"rebase":        "[branch \"topic\"]\nrebase = invalid\n",
		"pack window":   "[pack]\nwindow = invalid\n",
		"fetch refspec": "[remote \"origin\"]\nfetch = refs/heads/*:refs/remotes/origin/main\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseRepositoryConfig("[branch \"topic\"]\nmerge = refs/pull/83/head\n" + extra)
			require.Error(t, err)
		})
	}
	cfg, err := parseRepositoryConfig("[branch \"topic\"]\nmerge = refs/pull/83/head\n[extensions]\nunknown = true\n")
	require.NoError(t, err)
	require.ErrorContains(t, checkExtensions(cfg), "unknown extension")
}
