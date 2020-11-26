package pkg

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"

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
	fmt.Printf("%v pushed. Release scheduled\n", master)

	release, _, err := c.client.Repositories.GetLatestRelease(context.TODO(), owner, repo)
	if err != nil {
		return nil, err
	}

	return c.ReleaseCandidate(owner, repo, release.GetTagName(), master)
}

func (c *ClientImpl) HandleReleaseEvent(ev *github.ReleaseEvent) (interface{}, error) {
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
		return c.Promote(ev)
	}

	_, err = c.Cleanup(ev)
	if err != nil {
		return nil, err
	}

	curr := release.GetTagName()
	next := ev.GetRepo().GetDefaultBranch()
	comparison, _, err := c.client.Repositories.CompareCommits(context.TODO(), owner, repo, curr, next)
	if comparison.GetTotalCommits() != 0 {
		return c.ReleaseCandidate(ev.GetRepo().GetOwner().GetLogin(), ev.GetRepo().GetName(), ev.GetRelease().GetTagName(), ev.GetRepo().GetDefaultBranch())
	}
	return nil, nil
}

func (c *ClientImpl) Promote(ev *github.ReleaseEvent) (interface{}, error) {
	owner := ev.GetRepo().GetOwner().GetLogin()
	repo := ev.GetRepo().GetName()

	release := ev.GetRelease()

	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return nil, err
	}

	ref, _, err := c.client.Git.GetRef(context.TODO(), owner, repo, fmt.Sprintf("tags/%s", release.GetTagName()))
	if err != nil {
		return nil, err
	}
	newVersion, _ := version.SetPrerelease("")
	fmt.Printf("Creating tag %v with object %v\n", newVersion, ref.GetObject())
	_, _, err = c.client.Git.CreateRef(context.TODO(), owner, repo, &github.Reference{
		Ref:    github.String(fmt.Sprintf("refs/tags/v%v", newVersion)),
		Object: ref.Object,
	})
	if err != nil {
		return nil, err
	}
	fmt.Printf("Patching %v with TagName:%v Name:%v Commit:%v\n", release.GetID(), fmt.Sprintf("v%v", newVersion), newVersion.String(), release.GetTargetCommitish())
	_, _, err = c.client.Repositories.EditRelease(context.TODO(), owner, repo, release.GetID(), &github.RepositoryRelease{
		Name:    github.String(newVersion.String()),
		TagName: github.String(fmt.Sprintf("v%v", newVersion)),
	})

	if err != nil {
		return nil, err
	}

	_, err = c.LabelPRs(owner, repo, &newVersion)
	if err != nil {
		fmt.Println(err)
	}
	return nil, nil
}

func (c *ClientImpl) LabelPRs(owner, repo string, next *semver.Version) (interface{}, error) {
	last, err := c.FindLast(owner, repo, next)
	if err != nil {
		return nil, err
	}

	pulls, err := c.getPulls(owner, repo, fmt.Sprintf("v%v", last.String()), fmt.Sprintf("v%v", next.String()))
	if err != nil {
		return nil, err
	}

	fmt.Printf("Found %d pull requests\n", len(pulls))
	_, _, err = c.client.Issues.CreateLabel(context.TODO(), owner, repo, &github.Label{
		Name: github.String(next.String()),
	})
	if err != nil {
		return nil, err
	}

	for n, pull := range pulls {
		fmt.Printf("Labeling %d\n", pull.GetNumber())
		_, _, err := c.client.Issues.AddLabelsToIssue(context.TODO(), owner, repo, n, []string{next.String()})
		if err != nil {
			fmt.Println(err)
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

func (c *ClientImpl) ReleaseCandidate(owner, repo, latest, target string) (interface{}, error) {
	pulls, err := c.getPulls(owner, repo, latest, target)
	if err != nil {
		fmt.Printf("Error while examining pull requests, %v\n", err)
	}
	changelog, err := c.CollectChangelog(pulls)
	if err != nil {
		fmt.Println("Error while gathering changelog", err)
	}

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
	return nextTag, err
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
