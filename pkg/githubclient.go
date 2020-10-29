package pkg

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v32/github"
)

var (
	candidateRx, changelogRx *regexp.Regexp
)

type Client interface {
	HandlePushEvent(*github.PushEvent) (interface{}, error)
}

type ClientImpl struct {
	client *github.Client
}

func NewClient(tc *http.Client) Client {
	cl := github.NewClient(tc)

	return &ClientImpl{
		client: cl,
	}
}

func init() {
	changelogRx = regexp.MustCompile("```release-note\\r\\n([\\s\\S]*?)\\r\\n```")
	candidateRx = regexp.MustCompile("^rc.(?P<candidate>[0-9]+)$")
}

func (c *ClientImpl) HandlePushEvent(ev *github.PushEvent) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetName()
	repo := ev.GetRepo().GetName()
	pushed := strings.TrimPrefix(ev.GetRef(), "refs/heads/")
	master := ev.GetRepo().GetMasterBranch()

	if pushed != master {
		return nil, nil
	}

	release, _, err := c.client.Repositories.GetLatestRelease(context.TODO(), owner, repo)
	if err != nil {
		return nil, err
	}

	// Collect changelog
	comparison, _, err := c.client.Repositories.CompareCommits(context.TODO(), owner, repo, release.GetTargetCommitish(), ev.GetAfter())
	if err != nil {
		return nil, err
	}

	pulls, err := c.getPulls(owner, repo, comparison.Commits)

	logentries := []string{}
	for _, pull := range pulls {
		matches := changelogRx.FindStringSubmatch(pull.GetBody())
		if len(matches) < 2 {
			continue
		}
		logentries = append(logentries, fmt.Sprintf("- #%d %s", pull.GetNumber(), matches[1]))
	}

	// Calculate next tag
	v, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, err
	}
	con, err := semver.NewConstraint(fmt.Sprintf(">%v-rc.0 <%v-rc0", v.IncPatch(), v.IncPatch()))
	if err != nil {
		return nil, err
	}

	rc := 1
	page := 0
	for {
		tags, out, err := c.client.Repositories.ListTags(context.TODO(), owner, repo, &github.ListOptions{
			Page: page,
		})
		if err != nil {
			return nil, err
		}
		for _, t := range tags {
			n, err := semver.NewVersion(t.GetName())
			if err != nil {
				continue
			}
			if con.Check(n) {
				result := candidateRx.FindStringSubmatch(n.Prerelease())
				next, err := strconv.Atoi(result[1])
				if err != nil {
					return nil, err
				}
				if next >= rc {
					rc = next + 1
				}
			}
		}
		if out.NextPage == 0 {
			break
		}
		page = out.NextPage
	}
	nextTag := fmt.Sprintf("v%v-rc.%d", v.IncPatch(), rc)

	c.client.Repositories.CreateRelease(context.TODO(), owner, repo, &github.RepositoryRelease{
		TagName:         github.String(nextTag),
		Prerelease:      github.Bool(true),
		Name:            github.String(semver.MustParse(nextTag).String()),
		TargetCommitish: ev.After,
		Body:            github.String(fmt.Sprintf("Changes:\n\n%s", strings.Join(logentries, "\n"))),
	})

	return nextTag, nil
}

func (c *ClientImpl) getPulls(owner, repo string, commits []*github.RepositoryCommit) (map[int]*github.PullRequest, error) {
	pulls := make(map[int]*github.PullRequest)
	for _, commit := range commits {
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
