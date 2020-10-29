package pkg

import (
	"errors"
	"net/http"

	"github.com/google/go-github/v32/github"
	"github.com/labstack/echo/v4"
)

type WebhookHandler struct {
	Secret []byte
	Client Client
}

func NewHandler(client Client, secret []byte) *WebhookHandler {
	return &WebhookHandler{
		Client: client,
		Secret: secret,
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
		_, err := h.Client.HandlePushEvent(event)
		if err != nil {
			return err
		}
		return c.NoContent(http.StatusAccepted)
	case *github.PingEvent:
		return c.String(http.StatusOK, "Got ping event")
	}
	return errors.New("Not understood")
}
