package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v35/github"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"github.com/uniwise/go-ship-it/internal/scm"
)

type HandledGithubEvent interface {
	GetInstallation() *github.Installation
}

func (h *Handler) initReleaser(c echo.Context, ev HandledGithubEvent, repo scm.Repo, ref string, entry *logrus.Entry) (*scm.Releaser, error) {
	k := ghinstallation.NewFromAppsTransport(h.AppsTransport, ev.GetInstallation().GetID())
	client := scm.NewGithubClient(&http.Client{Transport: k, Timeout: time.Minute}, repo)

	return scm.NewReleaser(c.Request().Context(), client, ref, entry)
}

func (h *Handler) HandleGithub(c echo.Context, entry *logrus.Entry) error {
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
		l := entry.WithField("repo", event.GetRepo().GetFullName())
		r, err := h.initReleaser(c, event, event.GetRepo(), event.GetHead(), l)
		if err != nil {
			l.WithError(err).Error("Could not initialize releaser")

			return err
		}
		go r.HandlePush(context.Background(), event)

		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		l := entry.WithField("repo", event.GetRepo().GetFullName())
		r, err := h.initReleaser(c, event, event.GetRepo(), event.GetRelease().GetTagName(), l)
		if err != nil {
			l.WithError(err).Error("Could not initialize releaser")

			return err
		}
		go r.HandleRelease(context.Background(), event)

		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "pong")
	default:
		entry.Warn("Unexpected event")

		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}
