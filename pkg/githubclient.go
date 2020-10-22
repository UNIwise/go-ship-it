package pkg

import (
	"context"
	"os"

	"github.com/google/go-github/v32/github"
	"github.com/sirupsen/logrus"
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
	logrus.Debugf("Handling push event to %v", ev.GetBaseRef())
	return nil, nil
}
