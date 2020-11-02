package pkg

import (
	"net/http"

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
		client := NewClient(&http.Client{Transport: k})
		_, err := client.HandlePushEvent(event)
		if err != nil {
			return err
		}
		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		k := ghinstallation.NewFromAppsTransport(h.AppsTransport, event.Installation.GetID())
		client := NewClient(&http.Client{Transport: k})
		_, err := client.HandleReleaseEvent(event)
		if err != nil {
			return err
		}
		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "Got ping event")
	default:
		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}
