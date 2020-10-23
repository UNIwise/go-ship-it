package pkg

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	fmt.Printf("Master branch pushed. Release scheduled...\n")

	page := 0
	tags := []*github.RepositoryTag{}
	for {
		nexttags, out, err := c.client.Repositories.ListTags(context.Background(), owner, repo, &github.ListOptions{
			Page:    page,
			PerPage: 1,
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

	for _, tag := range tags {
		fmt.Printf("tag found: %v", tag.GetName())
	}

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
