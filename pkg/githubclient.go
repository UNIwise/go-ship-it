package pkg

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v32/github"
	"golang.org/x/oauth2"
)

type Client interface {
	HandlePushEvent(*github.PushEvent) (interface{}, error)
}

type ClientImpl struct {
	client *github.Client
}

func NewClient() Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{
			AccessToken: os.Getenv("GITHUB_TOKEN"),
		},
	)
	tc := oauth2.NewClient(ctx, ts)
	cl := github.NewClient(tc)

	return &ClientImpl{
		client: cl,
	}
}

func (c *ClientImpl) HandlePushEvent(ev *github.PushEvent) (interface{}, error) {

	fmt.Printf("Handling push event to %s on %s\n", ev.GetRepo().GetFullName(), ev.GetRef())

	owner := ev.GetRepo().GetOwner().GetName()
	repo := ev.GetRepo().GetName()
	pushed := strings.TrimPrefix(ev.GetRef(), "refs/heads/")
	master := ev.GetRepo().GetMasterBranch()

	if pushed != master {
		return nil, nil
	}
	fmt.Printf("Master branch pushed. pre-release scheduled...\n")

	tag, _, err := c.client.Repositories.GetLatestRelease(context.Background(), owner, repo)
	if err != nil {
		return nil, err
	}
	latest, err := semver.NewVersion(tag.GetTagName())
	if err != nil {
		return nil, err
	}

	comparison, _, err := c.client.Repositories.CompareCommits(context.Background(), owner, repo, tag.GetTargetCommitish(), ev.GetAfter())
	if err != nil {
		return nil, err
	}
	level := func() int {
		lvl := 1
		for _, commit := range comparison.Commits {
			prs, _, err := c.client.PullRequests.ListPullRequestsWithCommit(commit)
			if err != nil {
				return nil, err
			}
			for _, pr := range prs {
				for _, label := range pr.Labels {
					if label.GetName() == "major" {
						return 3
					} else if label.GetName() == "minor" {
						lvl = 2
					}
				}
			}
		}
		return lvl
	}()

	var next semver.Version
	switch level {
	case 2:
		next = latest.IncMinor()
	case 3:
		next = latest.IncMajor()
	default:
		next = latest.IncPatch()
	}

	con, err := semver.NewConstraint(fmt.Sprintf("> %s-rc.0 < %s", next.Original(), next.Original()))
	next.String()
	con.Check()

	page := 0
	tags := []*github.RepositoryTag{}
	for {
		nexttags, out, err := c.client.Repositories.ListTags(context.Background(), owner, repo, &github.ListOptions{
			Page: page,
		})
		if err != nil {
			return nil, err
		}
		tags = append(tags, nexttags...)
		if out.NextPage == 0 {
			break
		}
		page = out.NextPage
	}

	versions := semver.Collection{}
	for _, tag := range tags {
		max := semver.NewVersion(latest.GetTagName())
		fmt.Printf("tag found: %v", tag.GetName())
		version
		version, err := semver.NewVersion(tag.GetName())
		if err != nil {
			continue
		}
		versions = append(versions, version)
	}
	sort.Sort(versions)
	// Find latest release

	// Calculate new release version

	// Create new release

	// repo := ev.GetRepo()
	// repo.GetMasterBranch()

	// ref := ev.GetRef()

	// b, _, err := c.client.Repositories.GetBranch(context.Background(), repo.GetOwner().GetName(), repo.GetName(), repo.GetMasterBranch())

	// // Check if new release should be created
	// // repo := ev.GetRepo()

	// // ref := ev.GetRef()
	// // c.client.Repositories.GetBranch(context.Background(), repo.GetOwner().GetName(), repo.GetName(), )
	// // r, _, err := c.client.Repositories.Get(context.Background(), repo.GetOwner().GetName(), repo.GetName())
	// // reference, _, err := c.client.Git.GetRef(context.Background(), repo.GetOwner().GetName(), repo.GetName(), ref)
	// if err != nil {
	// 	return nil, err
	// }

	// c.client.Repositories.CreateRelease(context.Background(), repo.GetOwner().GetName(), repo.GetName(), &github.RepositoryRelease{
	// 	Prerelease: github.Bool(true),
	// 	Name:       github.String("2.18.5"),
	// 	TagName:    github.String("v2.18.5"),
	// })

	return nil, nil
}
