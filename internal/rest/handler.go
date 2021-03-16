package rest

import (
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v32/github"
	"github.com/labstack/echo/v4"
	"github.com/uniwise/go-ship-it/internal/scm"
	"gopkg.in/go-playground/validator.v9"
)

var validate *validator.Validate = validator.New()

type WebhookHandler struct {
	Secret        []byte
	AppsTransport *ghinstallation.AppsTransport
}

func NewHandler(atr *ghinstallation.AppsTransport, secret []byte) *WebhookHandler {
	return &WebhookHandler{
		AppsTransport: atr,
		Secret:        secret,
	}
}

type HandledGithubEvent interface {
	GetInstallation() *github.Installation
}

func (h *WebhookHandler) initGithubClient(c echo.Context, ev HandledGithubEvent, repo scm.Repo) *scm.GithubClientImpl {
	k := ghinstallation.NewFromAppsTransport(h.AppsTransport, ev.GetInstallation().GetID())
	client := scm.NewGithubClient(&http.Client{Transport: k, Timeout: time.Minute}, c.Logger(), repo)
	return client
}

func (h *WebhookHandler) HandleGithub(c echo.Context) error {
	payload, err := github.ValidatePayload(c.Request(), h.Secret)
	if err != nil {
		return err
	}
	event, err := github.ParseWebHook(github.WebHookType(c.Request()), payload)
	if err != nil {
		return err
	}

	switch event := event.(type) {
	case *github.PushEvent:
		client := h.initGithubClient(c, event, event.GetRepo())
		r, err := scm.NewReleaser(client, event.GetHead())
		if err != nil {
			return err
		}
		go r.HandlePush(event)
		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		client := h.initGithubClient(c, event, event.GetRepo())
		go client.HandleReleaseEvent(event)
		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "Got ping event")
	default:
		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}
