package pkg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v32/github"
)

var (
	candidateRx, changelogRx, emptyRx *regexp.Regexp
)

type Client interface {
	HandlePushEvent(*github.PushEvent, Config) (interface{}, error)
	HandleReleaseEvent(*github.ReleaseEvent, Config) (interface{}, error)
	GetFile(repo Repo, ref, file string) (io.ReadCloser, error)
}

type ClientImpl struct {
	client *github.Client
	log    echo.Logger
}

func NewClient(tc *http.Client, log echo.Logger) *ClientImpl {
	cl := github.NewClient(tc)

	return &ClientImpl{
		client: cl,
		log:    log,
	}
}

func init() {
	emptyRx = regexp.MustCompile("^\\s*((?i)none|\\s*)\\s*$")
	changelogRx = regexp.MustCompile("```release-note\\r\\n([\\s\\S]*?)\\r\\n```")
	candidateRx = regexp.MustCompile("^rc.(?P<candidate>[0-9]+)$")
}

func (c *ClientImpl) HandlePushEvent(ev *github.PushEvent, config Config) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()
	pushed := strings.TrimPrefix(ev.GetRef(), "refs/heads/")
	master := ev.GetRepo().GetMasterBranch()
	if config.TargetBranch != "" {
		master = config.TargetBranch
	}

	if pushed != master {
		return nil, nil
	}
	c.log.Infof("%v pushed. Scheduling release", master)

	release, _, err := c.client.Repositories.GetLatestRelease(context.TODO(), owner, repo)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get latest release")
	}

	return c.ReleaseCandidate(owner, repo, release.GetTagName(), master)
}

func (c *ClientImpl) HandleReleaseEvent(ev *github.ReleaseEvent, config Config) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()
	release := ev.GetRelease()
	if release.GetPrerelease() {
		return nil, nil
	}
	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, nil
	}
	if version.Prerelease() != "" {
		c.log.Infof("Promoting release %s", ev.GetRelease().GetName())

		return c.Promote(ev)
	}

	c.log.Infof("Cleaning up release candidates of %s", ev.GetRelease().GetName())
	_, err = c.Cleanup(ev)
	if err != nil {
		return nil, errors.Wrapf(err, "Error while cleaning up release candidates")
	}

	curr := release.GetTagName()
	next := ev.GetRepo().GetMasterBranch()
	if config.TargetBranch != "" {
		next = config.TargetBranch
	}
	comparison, _, err := c.client.Repositories.CompareCommits(context.TODO(), owner, repo, curr, next)
	if comparison.GetTotalCommits() != 0 {
		return c.ReleaseCandidate(ev.GetRepo().GetOwner().GetLogin(), ev.GetRepo().GetName(), ev.GetRelease().GetTagName(), next)
	}

	return nil, nil
}

func (c *ClientImpl) Promote(ev *github.ReleaseEvent) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()

	release := ev.GetRelease()

	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, errors.Wrapf(err, "Error while establishing semver from %s", release.GetTagName())
	}

	ref, _, err := c.client.Git.GetRef(context.TODO(), owner, repo, fmt.Sprintf("tags/%s", release.GetTagName()))
	if err != nil {
		return nil, errors.Wrapf(err, "Could not fetch tags tags/%s", release.GetTagName())
	}
	newVersion, _ := version.SetPrerelease("")
	c.log.Debugf("Creating tag v%v @ %s", newVersion, ref.GetObject().GetSHA())
	_, _, err = c.client.Git.CreateRef(context.TODO(), owner, repo, &github.Reference{
		Ref:    github.String(fmt.Sprintf("refs/tags/v%v", newVersion)),
		Object: ref.Object,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "Error while creating new tag v%v", newVersion)
	}
	c.log.Debugf("Updating release %v with tag v%v", release.GetID(), newVersion)
	_, _, err = c.client.Repositories.EditRelease(context.TODO(), owner, repo, release.GetID(), &github.RepositoryRelease{
		Name:    github.String(newVersion.String()),
		TagName: github.String(fmt.Sprintf("v%v", newVersion)),
	})

	if err != nil {
		return nil, errors.Wrapf(err, "Error while updating release %v with tag v%v", release.GetID(), newVersion)
	}

	c.log.Infof("Creating milestone for pull requests belonging to v%s", newVersion)
	_, err = c.LabelPRs(owner, repo, &newVersion)
	if err != nil {
		c.log.Warn("Error while labeling pull requests", err)
	}

	return nil, nil
}

func (c *ClientImpl) LabelPRs(owner, repo string, next *semver.Version) (interface{}, error) {
	last, err := c.FindLast(owner, repo, next)
	if err != nil {
		return nil, errors.Wrapf(err, "Could not find latest release candidate of v%s", next)
	}

	pulls, err := c.getPulls(owner, repo, fmt.Sprintf("v%s", last), fmt.Sprintf("v%s", next))
	if err != nil {
		return nil, errors.Wrapf(err, "Could not find pull requests associated with v%s", next)
	}

	c.log.Debugf("Found %d pull requests belonging to %s", len(pulls), next)
	milestone, _, err := c.client.Issues.CreateMilestone(context.TODO(), owner, repo, &github.Milestone{
		Title: github.String(next.String()),
		State: github.String("closed"),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "Could not create milestone %s", next)
	}

	for n, _ := range pulls {
		c.log.Debugf("Adding #%d to milestone %s", n, next.String())
		_, _, err := c.client.Issues.Edit(context.TODO(), owner, repo, n, &github.IssueRequest{
			Milestone: milestone.Number,
		})
		if err != nil {
			c.log.Warn("Error adding pull request to milestone ", err)
		}
	}
	return nil, nil
}

func (c *ClientImpl) FindLast(owner, repo string, next *semver.Version) (*semver.Version, error) {
	constraint, err := semver.NewConstraint(fmt.Sprintf("<%v", next.String()))
	if err != nil {
		return nil, err
	}

	refs, err := c.getRefs(owner, repo, "tags/v")
	if err != nil {
		return nil, err
	}

	top := semver.MustParse("v0.0.0")
	for _, ref := range refs {
		v, err := semver.NewVersion(strings.TrimPrefix(ref.GetRef(), "refs/tags/"))
		if err != nil {
			continue
		}
		if constraint.Check(v) && v.GreaterThan(top) {
			top = v
		}
	}
	return top, nil
}

func (c *ClientImpl) Cleanup(ev *github.ReleaseEvent) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()

	release := ev.GetRelease()

	refs, err := c.getRefs(owner, repo, fmt.Sprintf("tags/%v-rc.", release.GetTagName()))
	if err != nil {
		return nil, err
	}

	for _, r := range refs {
		tag := strings.TrimPrefix(r.GetRef(), "refs/tags/")
		toDelete, _, _ := c.client.Repositories.GetReleaseByTag(context.TODO(), owner, repo, tag)
		if toDelete != nil {
			if toDelete.GetID() == release.GetID() || !toDelete.GetPrerelease() {
				// Ensure full releases and current release is not inadvertently deleted
				continue
			}
			c.log.Infof("Deleting release %s", toDelete.GetTagName())
			_, err = c.client.Repositories.DeleteRelease(context.TODO(), owner, repo, toDelete.GetID())
			if err != nil {
				c.log.Warn("Could not delete release", err)
			}
		}
		_, err = c.client.Git.DeleteRef(context.TODO(), owner, repo, r.GetRef())
		if err != nil {
			c.log.Warn("Could not delete ref", err)
		}
	}

	return nil, nil
}

func (c *ClientImpl) ReleaseCandidate(owner, repo, latest, target string) (interface{}, error) {
	pulls, err := c.getPulls(owner, repo, latest, target)
	if err != nil {
		c.log.Warn("Error while examining pull requests", err)
	}
	changelog, err := c.CollectChangelog(pulls)
	if err != nil {
		c.log.Warn("Error while gathering changelog", err)
	}
	c.log.Debugf("Gathered changelog from %d pull requests", len(pulls))

	nextTag, err := c.NextTag(owner, repo, latest, pulls)
	if err != nil {
		return nil, errors.Wrap(err, "Could not calculate next tag")
	}

	_, _, err = c.client.Repositories.CreateRelease(context.TODO(), owner, repo, &github.RepositoryRelease{
		TagName:         github.String(nextTag),
		Prerelease:      github.Bool(true),
		Name:            github.String(semver.MustParse(nextTag).String()),
		TargetCommitish: github.String(target),
		Body:            github.String(changelog),
	})
	if err != nil {
		return nil, err
	}
	c.log.Infof("Release %s created", nextTag)
	return nextTag, nil
}

func (c *ClientImpl) CollectChangelog(pulls map[int]*github.PullRequest) (string, error) {
	logentries := []string{}
	for _, pull := range pulls {
		matches := changelogRx.FindStringSubmatch(pull.GetBody())
		if len(matches) < 2 {
			continue
		}
		if emptyRx.Match([]byte(matches[1])) {
			continue
		}
		logentries = append(logentries, fmt.Sprintf("- #%d %s", pull.GetNumber(), matches[1]))
	}
	return fmt.Sprintf("Changes:\n\n%s", strings.Join(logentries, "\n")), nil
}

func (c *ClientImpl) NextTag(owner, repo, latest string, pulls map[int]*github.PullRequest) (string, error) {
	v, err := semver.NewVersion(latest)
	if err != nil {
		return "", err
	}

	nextVersion := v.IncPatch()
out:
	for _, pr := range pulls {
		for _, label := range pr.Labels {
			if label.GetName() == "minor" {
				nextVersion = v.IncMinor()
			}
			if label.GetName() == "major" {
				nextVersion = v.IncMajor()
				break out
			}
		}
	}

	refs, err := c.getRefs(owner, repo, fmt.Sprintf("tags/v%v-rc.", nextVersion))
	if err != nil {
		return "", err
	}
	rc := 1
	for _, r := range refs {
		result := candidateRx.FindStringSubmatch(strings.TrimPrefix(r.GetRef(), fmt.Sprintf("refs/tags/v%v-", nextVersion)))
		next, err := strconv.Atoi(result[1])
		if err != nil {
			return "", err
		}
		if next >= rc {
			rc = next + 1
		}
	}

	return fmt.Sprintf("v%v-rc.%d", nextVersion, rc), nil
}

func (c *ClientImpl) getPulls(owner, repo, latest, current string) (map[int]*github.PullRequest, error) {
	comparison, _, err := c.client.Repositories.CompareCommits(context.TODO(), owner, repo, latest, current)
	if err != nil {
		return nil, err
	}
	pulls := make(map[int]*github.PullRequest)
	for _, commit := range comparison.Commits {
		page := 0
		for {
			prs, out, err := c.client.PullRequests.ListPullRequestsWithCommit(context.TODO(), owner, repo, commit.GetSHA(), &github.PullRequestListOptions{ListOptions: github.ListOptions{Page: page}})
			if err != nil {
				return nil, err
			}
			for _, pr := range prs {
				if pr.GetNumber() != 0 {
					pulls[pr.GetNumber()] = pr
				} else {
					return nil, errors.New("Could not get pull request number")
				}
			}
			if out.NextPage == 0 {
				break
			}
			page = out.NextPage
		}
	}
	return pulls, nil
}

func (c *ClientImpl) getRefs(owner, repo, prefix string) ([]*github.Reference, error) {
	page := 0
	references := []*github.Reference{}
	for {
		refs, out, err := c.client.Git.ListMatchingRefs(context.TODO(), owner, repo, &github.ReferenceListOptions{
			Ref: prefix,
			ListOptions: github.ListOptions{
				Page: page,
			},
		})
		if err != nil {
			return nil, err
		}
		references = append(references, refs...)
		if out.NextPage == 0 {
			break
		}
		page = out.NextPage
	}
	return references, nil
}

func (c *ClientImpl) GetFile(repo Repo, ref, file string) (io.ReadCloser, error) {
	return c.client.Repositories.DownloadContents(context.TODO(), repo.GetOwner().GetLogin(), repo.GetName(), file, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
}
