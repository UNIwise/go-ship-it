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
		return nil, errors.Wrap(err, "Failed to decode config file")
	}
	if err := configValidator.Struct(config); err != nil {
		return nil, errors.Wrap(err, "Failed to validate configuration")
	}
	return config, nil
}

func NewReleaser(client GithubClient, ref string, log *logrus.Entry) (*Releaser, error) {
	config, err := getConfig(client, ref)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to configure releaser")
	}
	return &Releaser{
		client: client,
		config: config,
		log:    log,
	}, nil
}

func (r *Releaser) HandlePush(e *github.PushEvent) {
	if !r.Match(e.GetRef()) {
		return
	}

	r.log.Infof("%s pushed. Releasing...", e.GetRef())
	t, v, err := r.client.GetLatestTag()
	if err != nil {
		r.log.WithError(err).Error("Failed to get latest release")
		return
	}

	r.log.Debugf("Finding commits in range %s..%.7s", t, e.GetAfter())
	comparison, err := r.client.GetCommitRange(t, e.GetAfter())
	if err != nil {
		r.log.WithError(err).Error("Failed to get commit range")
		return
	}

	r.log.Debugf("Finding PRs in %d commits", len(comparison.Commits))
	pulls, err := r.client.GetPullsInCommitRange(comparison.Commits)
	if err != nil {
		r.log.WithError(err).Error("Failed to get pull requests in commit range")
		return
	}

	r.log.Debugf("Finding next version based on %d PRs", len(pulls))
	next, err := r.Increment(v, pulls)
	if err != nil {
		r.log.WithError(err).Error("Failed to increment version")
		return
	}
	tagname, name := fmt.Sprintf("v%s", next.String()), next.String()

	r.log.Debugf("Collecting changelog from %d PRs", len(pulls))
	changelog, err := r.CollectChangelog(pulls)
	if err != nil {
		r.log.WithError(err).Error("Failed to collect changelog")
		return
	}

	r.log.Debugf("Creating tag '%s' at '%.7s'", tagname, e.GetAfter())
	err = r.client.CreateRef(&github.Reference{
		Ref: github.String(fmt.Sprintf("refs/tags/%s", tagname)),
		Object: &github.GitObject{
			SHA: github.String(e.GetAfter()),
		},
	})
	if err != nil {
		r.log.WithError(err).Error("Failed to create reference")
		return
	}

	r.log.WithFields(logrus.Fields{
		"Name":       name,
		"TagName":    tagname,
		"Commitish":  strings.TrimPrefix(e.GetRef(), "refs/heads/"),
		"Prerelease": r.config.Strategy.Type == "pre-release",
	}).Debugf("Creating release")
	err = r.client.CreateRelease(&github.RepositoryRelease{
		TagName:         github.String(tagname),
		Name:            github.String(name),
		TargetCommitish: github.String(strings.TrimPrefix(e.GetRef(), "refs/heads/")),
		Prerelease:      github.Bool(r.config.Strategy.Type == "pre-release"),
		Body:            github.String(changelog),
	})
	if err != nil {
		r.log.WithError(err).Error("Failed to create release")
		return
	}
	r.log.Infof("Release %s created", tagname)
}

func (r *Releaser) HandleRelease(e *github.ReleaseEvent) {
	if r.config.Strategy.Type == "full-release" {
		return
	}
	version, err := semver.NewVersion(e.GetRelease().GetTagName())
	if err != nil {
		r.log.WithError(err).Errorf("Failed to parse tag '%s' as version", e.GetRelease().GetTagName())
		return
	}
	// Promotion action
	if version.Prerelease() != "" && !e.GetRelease().GetPrerelease() {
		r.log.Infof("Promoting release '%s'", e.GetRelease().GetTagName())
		n, err := r.Promote(e.GetRelease())
		if err != nil {
			r.log.WithError(err).Errorf("Failed to promote release '%d'", e.GetRelease().GetID())
			return
		}
		r.log.Infof("Release promoted to '%s'", n.GetTagName())

		r.log.Info("Adding pull requests to milestone")
		current, err := semver.NewVersion(n.GetTagName())
		if err != nil {
			r.log.WithError(err).Error("Failed to parse tag '%s' as version", n.GetTagName())
			return
		}
		r.log.Debugf("Finding previous release based on '%s'", current.String())
		previous, err := r.FindPreviousRelease(current)
		if err != nil {
			r.log.WithError(err).Errorf("Failed to find previous release based on '%s'", current.String())
			return
		}

		r.log.Debugf("Finding commits in range %s..%s", previous.GetTagName(), n.GetTagName())
		comparison, err := r.client.GetCommitRange(previous.GetTagName(), n.GetTagName())
		if err != nil {
			r.log.WithError(err).Error("Failed to get commit range")
			return
		}

		r.log.Debugf("Finding PRs in %d commits", len(comparison.Commits))
		pulls, err := r.client.GetPullsInCommitRange(comparison.Commits)
		if err != nil {
			r.log.WithError(err).Error("Failed to get pull requests in commit range")
			return
		}

		r.log.Debugf("Creating milestone '%s'", n.GetName())
		milestone, err := r.client.CreateMilestone(n.GetName())
		if err != nil {
			r.log.WithError(err).Errorf("Failed to create milestone '%s'", milestone.GetTitle())
			return
		}

		r.log.Debugf("Adding %d pull requests to milestone '%s'", len(pulls), milestone.GetTitle())
		failed := 0
		for _, p := range pulls {
			err := r.client.AddPRtoMilestone(p, milestone)
			if err != nil {
				r.log.WithError(err).Warnf("Failed to add PR '%d' to milestone '%d'", p.GetNumber(), milestone.GetNumber())
				failed++
			}
		}
		r.log.Infof("%d pull requests added to milestone '%s'", len(pulls)-failed, milestone.GetTitle())
		return
	}
	// Cleanup action
	if version.Prerelease() == "" && !e.GetRelease().GetPrerelease() {
		r.log.Infof("Cleaning up candidates of '%s'", e.GetRelease().GetTagName())
		number, err := r.CleanupCandidates(e.GetRelease())
		if err != nil {
			r.log.WithError(err).Errorf("Failed to clean up candidates for release '%d'", e.GetRelease().GetID())
			return
		}
		r.log.Infof("Removed %d release candidates", number)
		return
	}
}

func (r *Releaser) FindPreviousRelease(version *semver.Version) (*github.RepositoryRelease, error) {
	constraint, err := semver.NewConstraint(fmt.Sprintf("<%s", version.String()))
	if err != nil {
		return nil, errors.Wrap(err, "Could not create semver constraint")
	}
	pattern := fmt.Sprintf("v%d.%d.", version.Major(), version.Minor())
	if version.Patch() == 0 {
		pattern = fmt.Sprintf("v%d.", version.Major())
	}
	if version.Minor() == 0 {
		pattern = "v"
	}

	refs, err := r.client.GetRefs(fmt.Sprintf("tags/%s", pattern))
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to list references with pattern 'tags/%s'", pattern)
	}
	top := semver.MustParse("v0.0.0")
	for _, ref := range refs {
		tag := strings.TrimPrefix(ref.GetRef(), "refs/tags/")
		v, err := semver.NewVersion(tag)
		if err != nil {
			continue
		}
		if !constraint.Check(v) {
			continue
		}
		if v.GreaterThan(top) {
			top = v
		}
	}
	return r.client.GetReleaseByTag(top.Original())
}

func (r *Releaser) Promote(release *github.RepositoryRelease) (*github.RepositoryRelease, error) {
	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse tag '%s' as semantic version", release.GetTagName())
	}

	full, err := version.SetPrerelease("")
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to unset prerelease for tag '%s'", release.GetTagName())
	}

	ref, err := r.client.GetRef(fmt.Sprintf("tags/%s", release.GetTagName()))
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get reference to tag '%s'", release.GetTagName())
	}

	err = r.client.CreateRef(&github.Reference{
		Ref: github.String(fmt.Sprintf("tags/v%s", full.String())),
		Object: &github.GitObject{
			SHA: github.String(ref.GetObject().GetSHA()),
		},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create reference '%s'", full.String())
	}

	rel, err := r.client.EditRelease(release.GetID(), &github.RepositoryRelease{
		TagName: github.String(fmt.Sprintf("v%s", full.String())),
		Name:    github.String(full.String()),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to edit release '%d'", release.GetID())
	}
	return rel, nil
}

func (r *Releaser) CleanupCandidates(release *github.RepositoryRelease) (int, error) {
	refs, err := r.client.GetRefs(fmt.Sprintf("tags/%s-rc.", release.GetTagName()))
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to list refs for tag '%s'", release.GetTagName())
	}

	for _, ref := range refs {
		tag := strings.TrimPrefix(ref.GetRef(), "refs/tags/")
		if doomed, _ := r.client.GetReleaseByTag(tag); doomed != nil {
			if doomed.GetID() == release.GetID() || !doomed.GetPrerelease() {
				continue
			}
			if err := r.client.DeleteRelease(doomed); err != nil {
				r.log.WithError(err).Warnf("Failed to delete release '%d'. Continuing...", doomed.GetID())
			}
		}
		if err := r.client.DeleteTag(tag); err != nil {
			r.log.WithError(err).Warnf("Failed to delete tag '%s'. Continuing...", tag)
		}
	}

	return len(refs), nil
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
		return nil, errors.Wrap(err, "Failed to retrieve pre-releases")
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
