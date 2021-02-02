package pkg

import (
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v32/github"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
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

type HandledEvent interface {
	GetInstallation() *github.Installation
}

type Repo interface {
	GetOwner() *github.User
	GetName() string
}

func (h *WebhookHandler) initClient(c echo.Context, ev HandledEvent, prefix string) (echo.Logger, *ClientImpl) {
	k := ghinstallation.NewFromAppsTransport(h.AppsTransport, ev.GetInstallation().GetID())
	l := c.Logger()
	l.SetPrefix(prefix)
	client := NewClient(&http.Client{Transport: k, Timeout: time.Minute}, l)
	return l, client
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
		logger, client := h.initClient(c, event, event.GetRepo().GetFullName())
		go handlePushEvent(logger, client, event)
		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		logger, client := h.initClient(c, event, event.GetRepo().GetFullName())
		go handleReleaseEvent(logger, client, event)
		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "Got ping event")
	default:
		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}

func handleReleaseEvent(l echo.Logger, client Client, ev *github.ReleaseEvent) {
	l.Debug("Handling release event")
	config, err := getConfig(l, client, ev.GetRepo(), ev.GetRelease().GetTargetCommitish())
	if err != nil {
		l.Error("Could not instantiate repository config ", err)
		return
	}
	if config == nil {
		l.Error("Config is nil")
		return
	}
	if _, err := client.HandleReleaseEvent(ev, *config); err != nil {
		l.Error("Error handling release event", err)
	}
}

func handlePushEvent(l echo.Logger, client Client, ev *github.PushEvent) {
	l.Debug("Handling push event")
	config, err := getConfig(l, client, ev.GetRepo(), ev.GetAfter())
	if err != nil {
		l.Error("Could not instantiate repository config ", err)
		return
	}
	if config == nil {
		l.Error("Config is nil")
		return
	}

	if _, err := client.HandlePushEvent(ev, *config); err != nil {
		l.Error("Error handling push event", err)
	}
}

type Config struct {
	TargetBranch string `yaml:"targetBranch,omitempty"`
}

func getConfig(l echo.Logger, client Client, repo Repo, ref string) (*Config, error) {
	config := &Config{
		TargetBranch: "",
	}
	reader, err := client.GetFile(repo, ref, ".ship-it")
	if err != nil {
		l.Debug("Error getting config from github, using defaults ", err)
		return config, nil
	}
	defer reader.Close()
	decoder := yaml.NewDecoder(reader)
	if err := decoder.Decode(config); err != nil {
		return nil, errors.Wrap(err, "Error decoding config file")
	}
	if err := config.validate(); err != nil {
		return nil, errors.Wrap(err, "Failed to validate config file")
	}
	return config, nil
}

func (c *Config) validate() error {
	return nil
}
