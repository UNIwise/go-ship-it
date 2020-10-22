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

func NewHandler(secret []byte) *WebhookHandler {
	return &WebhookHandler{
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
			return c.NoContent(http.StatusAccepted)
		}
		return err
	case *github.PingEvent:
		return c.NoContent(http.StatusOK)
	}
	return errors.New("Not understood")
}
