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
	candidateRx, changelogRx, emptyRx *regexp.Regexp
)

type Client interface {
	HandlePushEvent(*github.PushEvent) (interface{}, error)
	HandleReleaseEvent(*github.ReleaseEvent) (interface{}, error)
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
	emptyRx = regexp.MustCompile("^\\s*((?i)none|\\s*)\\s*$")
	changelogRx = regexp.MustCompile("```release-note\\r\\n([\\s\\S]*?)\\r\\n```")
	candidateRx = regexp.MustCompile("^rc.(?P<candidate>[0-9]+)$")
}

func (c *ClientImpl) HandlePushEvent(ev *github.PushEvent) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()
	pushed := strings.TrimPrefix(ev.GetRef(), "refs/heads/")
	master := ev.GetRepo().GetMasterBranch()

	if pushed != master {
		return nil, nil
	}
	fmt.Println("master pushed. Release scheduled")

	release, _, err := c.client.Repositories.GetLatestRelease(context.TODO(), owner, repo)
	if err != nil {
		return nil, err
	}

	// Collect changelog
	comparison, _, err := c.client.Repositories.CompareCommits(context.TODO(), owner, repo, release.GetTagName(), ev.GetAfter())
	if err != nil {
		return nil, err
	}

	pulls, err := c.getPulls(owner, repo, comparison.Commits)

	if err != nil {
		fmt.Printf("Error while examining pull requests, %v\n", err)
	}

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

	// Calculate next tag
	v, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, err
	}

	refs, err := c.getRefs(owner, repo, fmt.Sprintf("tags/v%v-rc.", v.IncPatch()))
	if err != nil {
		return nil, err
	}
	rc := 1
	for _, r := range refs {
		result := candidateRx.FindStringSubmatch(strings.TrimPrefix(r.GetRef(), fmt.Sprintf("refs/tags/v%v-", v.IncPatch())))
		next, err := strconv.Atoi(result[1])
		if err != nil {
			return nil, err
		}
		if next >= rc {
			rc = next + 1
		}
	}

	nextTag := fmt.Sprintf("v%v-rc.%d", v.IncPatch(), rc)

	_, _, err = c.client.Repositories.CreateRelease(context.TODO(), owner, repo, &github.RepositoryRelease{
		TagName:         github.String(nextTag),
		Prerelease:      github.Bool(true),
		Name:            github.String(semver.MustParse(nextTag).String()),
		TargetCommitish: ev.After,
		Body:            github.String(fmt.Sprintf("Changes:\n\n%s", strings.Join(logentries, "\n"))),
	})
	return nextTag, err
}

func (c *ClientImpl) HandleReleaseEvent(ev *github.ReleaseEvent) (interface{}, error) {
	release := ev.GetRelease()
	if release.GetPrerelease() {
		return nil, nil
	}
	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, nil
	}
	if version.Prerelease() != "" {
		return c.Promote(ev)
	}
	return c.Cleanup(ev)
}

func (c *ClientImpl) Promote(ev *github.ReleaseEvent) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()

	release := ev.GetRelease()

	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, err
	}

	newVersion, err := version.SetPrerelease("")
	fmt.Printf("Patching %v with TagName:%v Name:%v Commit:%v\n", release.GetID(), fmt.Sprintf("v%v", newVersion), newVersion.String(), release.GetTargetCommitish())
	_, _, err = c.client.Repositories.EditRelease(context.TODO(), owner, repo, release.GetID(), &github.RepositoryRelease{
		TagName:         github.String(fmt.Sprintf("v%v", newVersion)),
		Name:            github.String(newVersion.String()),
		TargetCommitish: release.TargetCommitish,
	})
	if err != nil {
		return nil, err
	}
	return nil, nil
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
			_, err = c.client.Repositories.DeleteRelease(context.TODO(), owner, repo, toDelete.GetID())
			if err != nil {
				fmt.Println(err)
			}
		}
		_, err = c.client.Git.DeleteRef(context.TODO(), owner, repo, r.GetRef())
		if err != nil {
			fmt.Println(err)
		}
	}

	return nil, nil
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
