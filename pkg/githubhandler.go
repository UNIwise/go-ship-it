package pkg

import (
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v32/github"
	"github.com/labstack/echo/v4"
)

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
		k := ghinstallation.NewFromAppsTransport(h.AppsTransport, event.Installation.GetID())
		c.Logger().SetPrefix(event.GetRepo().GetFullName())
		client := NewClient(&http.Client{Transport: k, Timeout: time.Minute}, c.Logger())
		go handlePushEvent(c, client, event)
		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		k := ghinstallation.NewFromAppsTransport(h.AppsTransport, event.Installation.GetID())
		c.Logger().SetPrefix(event.GetRepo().GetFullName())
		client := NewClient(&http.Client{Transport: k, Timeout: time.Minute}, c.Logger())
		go handleReleaseEvent(c, client, event)
		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "Got ping event")
	default:
		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}

func handleReleaseEvent(c echo.Context, client Client, ev *github.ReleaseEvent) {
	c.Logger().Debug("Handling release event")
	_, err := client.HandleReleaseEvent(ev)
	if err != nil {
		c.Logger().Error("Error handling release event", err)
	}
}

func handlePushEvent(c echo.Context, client Client, ev *github.PushEvent) {
	c.Logger().Debug("Handling push event")
	_, err := client.HandlePushEvent(ev)
	if err != nil {
		c.Logger().Error("Error handling push event", err)
	}
}
