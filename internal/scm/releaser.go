package scm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v33/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	validator "gopkg.in/go-playground/validator.v9"
	"gopkg.in/yaml.v2"
)

type Releaser struct {
	client GithubClient
	config *Config
	log    *logrus.Entry
}

var (
	configValidator = validator.New()
	candidateRx     = regexp.MustCompile("^rc.(?P<candidate>[0-9]+)$")
	changelogRx     = regexp.MustCompile("```release-note([\\s\\S]*?)```")
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

func NewReleaser(client GithubClient, ref string, log *logrus.Entry) (*Releaser, error) {
	config, err := getConfig(client, ref)
	if err != nil {
		return nil, errors.Wrap(err, "error configuring releaser")
	}
	return &Releaser{
		client: client,
		config: config,
		log:    log,
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

	changelog, err := r.CollectChangelog(pulls)
	if err != nil {
		return errors.Wrap(err, "Could not collect changelog")
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

	err = r.client.CreateRelease(&github.RepositoryRelease{
		TagName:         github.String(fmt.Sprintf("v%s", next.String())),
		Name:            github.String(next.String()),
		TargetCommitish: github.String(strings.TrimPrefix(e.GetRef(), "refs/heads/")),
		Prerelease:      github.Bool(r.config.Strategy.Type == "pre-release"),
		Body:            github.String(changelog),
	})
	if err != nil {
		return errors.Wrap(err, "Failed to create release")
	}
	return nil
}

func (r *Releaser) HandleRelease(e *github.ReleaseEvent) error {
	if r.config.Strategy.Type == "full-release" {
		return nil
	}
	version, err := semver.NewVersion(e.GetRelease().GetTagName())
	if err != nil {
		return errors.Wrapf(err, "Could not parse tag '%s' as version", e.GetRelease().GetTagName())
	}
	if version.Prerelease() != "" && !e.GetRelease().GetPrerelease() {
		return r.Promote(e.GetRelease())
	}
	if version.Prerelease() == "" && !e.GetRelease().GetPrerelease() {
		return r.CleanupCandidates(e.GetRelease())
	}
	return nil
}

func (r *Releaser) Promote(release *github.RepositoryRelease) error {
	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return errors.Wrapf(err, "Could parse tag '%s' as semantic version", release.GetTagName())
	}

	full, err := version.SetPrerelease("")
	if err != nil {
		return errors.Wrapf(err, "Failed to unset prerelease for tag '%s'", release.GetTagName())
	}

	ref, err := r.client.GetRef(fmt.Sprintf("tags/%s", release.GetTagName()))
	if err != nil {
		return errors.Wrapf(err, "Could not get reference to tag '%s'", release.GetTagName())
	}

	err = r.client.CreateRef(&github.Reference{
		Ref: github.String(fmt.Sprintf("tags/v%s", full.String())),
		Object: &github.GitObject{
			SHA: github.String(ref.GetObject().GetSHA()),
		},
	})
	if err != nil {
		return errors.Wrapf(err, "Could not create reference '%s'", full.String())
	}

	err = r.client.EditRelease(release.GetID(), &github.RepositoryRelease{
		TagName: github.String(fmt.Sprintf("v%s", full.String())),
		Name:    github.String(full.String()),
	})
	if err != nil {
		return errors.Wrapf(err, "Could not edit release '%d'", release.GetID())
	}
	return nil
}

func (r *Releaser) CleanupCandidates(release *github.RepositoryRelease) error {
	refs, err := r.client.GetRefs(fmt.Sprintf("tags/v%s-rc.", release.GetTagName()))
	if err != nil {
		return errors.Wrapf(err, "Could not list refs for tag '%s'", release.GetTagName())
	}

	for _, ref := range refs {
		tag := strings.TrimPrefix(ref.GetRef(), "refs/tags/")
		doomed, _ := r.client.GetReleaseByTag(tag)
		if doomed != nil {
			if doomed.GetID() == release.GetID() || !doomed.GetPrerelease() {
				continue
			}

			if err := r.client.DeleteRelease(doomed); err != nil {
				// Warn
			}
		}
		if err := r.client.DeleteTag(tag); err != nil {
			// Warn
		}
	}

	return nil
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
			return nil, errors.Wrap(err, "Failed to read pre-release number")
		}
		if nextrc >= rc {
			rc = nextrc + 1
		}
	}

	return semver.NewVersion(fmt.Sprintf("v%s-rc.%d", next, rc))
}

func (r *Releaser) CollectChangelog(pulls []*github.PullRequest) (string, error) {
	logs := []string{}
	for _, p := range pulls {
		matches := changelogRx.FindStringSubmatch(p.GetBody())
		if len(matches) < 2 {
			continue
		}
		desc := strings.TrimSpace(matches[1])
		if strings.ToLower(desc) != "none" || desc != "" {
			logs = append(logs, fmt.Sprintf("- #%d %s", p.GetNumber(), desc))
		}
	}
	return fmt.Sprintf("Changes:\n\n%s", strings.Join(logs, "\n")), nil
}
