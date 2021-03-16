package scm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v32/github"
	"github.com/pkg/errors"
	validator "gopkg.in/go-playground/validator.v9"
	"gopkg.in/yaml.v2"
)

type Releaser struct {
	client GithubClient
	config *Config
}

var (
	configValidator = validator.New()
	candidateRx     = regexp.MustCompile("^rc.(?P<candidate>[0-9]+)$")
)

type LabelsConfig struct {
	Major string `yaml:"major,omitempty"`
	Minor string `yaml:"minor,omitempty"`
}

type StrategyConf struct {
	Type string `yaml:"type,omitempty" validate:"oneof=pre-release full-release"`
}

type Config struct {
	TargetBranch string       `yaml:"targetBranch,omitempty" validate:"required"`
	Labels       LabelsConfig `yaml:"labels,omitempty"`
	Strategy     StrategyConf `yaml:"strategy,omitempty"`
}

func getConfig(c GithubClient, ref string) (*Config, error) {
	config := &Config{
		TargetBranch: c.GetRepo().GetDefaultBranch(),
		Labels: LabelsConfig{
			Major: "major",
			Minor: "minor",
		},
		Strategy: StrategyConf{
			Type: "pre-release",
		},
	}
	reader, err := c.GetFile(ref, ".ship-it")
	if err != nil {
		return config, nil
	}
	defer reader.Close()
	decoder := yaml.NewDecoder(reader)
	if err := decoder.Decode(config); err != nil {
		return nil, errors.Wrap(err, "Error decoding config file")
	}
	if err := configValidator.Struct(config); err != nil {
		return nil, errors.Wrap(err, "Could not validate configuration")
	}
	return config, nil
}

func NewReleaser(client GithubClient, ref string) (*Releaser, error) {
	config, err := getConfig(client, ref)
	if err != nil {
		return nil, err
	}
	return &Releaser{
		client: client,
		config: config,
	}, nil
}

func (r *Releaser) HandlePush(e *github.PushEvent) error {
	if !r.Match(e.GetRef()) {
		return nil
	}

	t, v, err := r.client.GetLatestTag()
	if err != nil {
		return errors.Wrap(err, "Could not get latest release")
	}

	comparison, err := r.client.GetCommitRange(t, e.GetHead())
	if err != nil {
		return errors.Wrap(err, "Could not get commit range")
	}

	pulls, err := r.client.GetPullsInCommitRange(comparison.Commits)
	if err != nil {
		return errors.Wrap(err, "Could not get pull requests in commit range")
	}

	next, err := r.Increment(v, pulls)
	if err != nil {
		return errors.Wrap(err, "Could not increment version")
	}

	err = r.client.CreateRef(&github.Reference{
		Ref: github.String(fmt.Sprintf("refs/tags/v%s", next.String())),
		Object: &github.GitObject{
			SHA: github.String(e.GetHead()),
		},
	})
	if err != nil {
		return errors.Wrap(err, "Failed to create reference")
	}

	r.client.CreateRelease(&github.RepositoryRelease{
		TagName: github.String(fmt.Sprintf("v%s", next.String())),
		Name: github.String(next.String()),
		TargetCommitish: github.String(strings.TrimPrefix(e.GetRef(), "refs/heads/")),
	})




	// 	next := v.IncPatch()
	// out:
	// 	for _, p := range pulls {
	// 		for _, l := range p.Labels {
	// 			switch l.GetName() {
	// 			case r.config.Labels.Minor:
	// 				next = v.IncMinor()
	// 			case r.config.Labels.Major:
	// 				next = v.IncMajor()
	// 				break out
	// 			}
	// 		}
	// 	}

	// if r.config.Strategy.Type == "pre-release" {
	// 	refs, err := r.client.GetRefs(fmt.Sprintf("tags/v%s-rc.", next))
	// 	if err != nil {
	// 		return errors.Wrap(err, "Error finding references")
	// 	}

	// }

}

func (r *Releaser) HandleRelease() {

}

func (r *Releaser) Match(ref string) bool {
	return strings.TrimPrefix(ref, "refs/heads/") == r.config.TargetBranch
}

func (r *Releaser) Increment(current *semver.Version, pulls []*github.PullRequest) (*semver.Version, error) {
	next := current.IncPatch()
out:
	for _, p := range pulls {
		for _, l := range p.Labels {
			switch l.GetName() {
			case r.config.Labels.Minor:
				next = current.IncMinor()
			case r.config.Labels.Major:
				next = current.IncMajor()
				break out
			}
		}
	}

	if r.config.Strategy.Type == "full-release" {
		return &next, nil
	}

	prereleases, err := r.client.GetRefs(fmt.Sprintf("tags/v%s-rc.", next.String()))
	if err != nil {
		return nil, errors.Wrap(err, "Error retrieving pre releases")
	}

	rc := 1
	for _, r := range prereleases {
		result := candidateRx.FindStringSubmatch(strings.TrimPrefix(r.GetRef(), fmt.Sprintf("refs/tags/v%s-", next)))
		nextrc, err := strconv.Atoi(result[1])
		if err != nil {
			return nil, err
		}
		if nextrc >= rc {
			rc = nextrc + 1
		}
	}

	return semver.NewVersion(fmt.Sprintf("v%s-rc.%d", next, rc))
}

func sort()
