package rest

import (
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v33/github"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"github.com/uniwise/go-ship-it/internal/scm"
	"gopkg.in/go-playground/validator.v9"
)

var validate *validator.Validate = validator.New()

type WebhookHandler struct {
	Secret        []byte
	AppsTransport *ghinstallation.AppsTransport
	Logger        *logrus.Entry
}

func NewHandler(atr *ghinstallation.AppsTransport, secret []byte, logger *logrus.Entry) *WebhookHandler {
	return &WebhookHandler{
		AppsTransport: atr,
		Secret:        secret,
		Logger:        logger,
	}
}

type HandledGithubEvent interface {
	GetInstallation() *github.Installation
}

func (h *WebhookHandler) initReleaser(c echo.Context, ev HandledGithubEvent, repo scm.Repo, ref string, entry *logrus.Entry) (*scm.Releaser, error) {
	k := ghinstallation.NewFromAppsTransport(h.AppsTransport, ev.GetInstallation().GetID())
	client := scm.NewGithubClient(&http.Client{Transport: k, Timeout: time.Minute}, repo)

	return scm.NewReleaser(client, ref, entry.WithField("repo", repo.GetFullName()))
}

func (h *WebhookHandler) HandleGithub(c echo.Context) error {
	id := c.Response().Header().Get(echo.HeaderXRequestID)
	entry := h.Logger.WithField("id", id)

	payload, err := github.ValidatePayload(c.Request(), h.Secret)
	if err != nil {
		return echo.ErrBadRequest.SetInternal(err)
	}

	event, err := github.ParseWebHook(github.WebHookType(c.Request()), payload)
	if err != nil {
		return echo.ErrBadRequest.SetInternal(err)
	}

	switch event := event.(type) {
	case *github.PushEvent:
		r, err := h.initReleaser(c, event, event.GetRepo(), event.GetHead(), entry)
		if err != nil {
			entry.WithError(err).Error("Could not initialize releaser")

			return err
		}
		go r.HandlePush(event)

		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		r, err := h.initReleaser(c, event, event.GetRepo(), event.GetRelease().GetTagName(), entry)
		if err != nil {
			entry.WithError(err).Error("Could not initialize releaser")

			return err
		}
		go r.HandleRelease(event)

		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "pong")
	default:
		entry.WithField("event", ).Warn("Unexpected event")

		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}
