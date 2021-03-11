package rest

import (
	"net/http"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v32/github"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github.com/uniwise/go-ship-it/internal/scm"
	"gopkg.in/go-playground/validator.v9"
	"gopkg.in/yaml.v2"
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

		// go handlePushEvent(logger, client, event)
		return c.String(http.StatusAccepted, "Handling push event")
	case *github.ReleaseEvent:
		client := h.initGithubClient(c, event, event.GetRepo())

		// go client.HandleReleaseEvent()
		// go handleReleaseEvent(logger, client, event)
		return c.String(http.StatusAccepted, "Handling release event")
	case *github.PingEvent:
		return c.String(http.StatusOK, "Got ping event")
	default:
		return c.String(http.StatusNotAcceptable, "Unexpected event")
	}
}

func handleReleaseEvent(l echo.Logger, client scm.Client, ev *github.ReleaseEvent) {
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

func handlePushEvent(l echo.Logger, client scm.Client, ev *github.PushEvent) {
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

	if strings.TrimPrefix(ev.GetRef(), "refs/heads/") != config.TargetBranch {
		return
	}

	l.Infof("%s pushed. Releasing...", config.TargetBranch)

	release, err := client.GetLatestTag(ev.GetRepo())
	if err != nil {
		l.Error("Could not get latest release ", err)
		return
	}

	comparison, err := client.GetCommitRange(ev.GetRepo(), release.Original(), ev.GetAfter())
	if err != nil {
		l.Errorf("Could not get commit comparison between '%s' and '%s' err=%v", release.Original(), ev.GetAfter(), err)
		return
	}

	pulls, err := client.GetPullsInCommitRange(ev.GetRepo(), comparison.Commits)
	if err != nil {
		l.Error("Could not get pull requests in commit range ", err)
		return
	}

	if _, err := client.HandlePushEvent(ev, *config); err != nil {
		l.Error("Error handling push event", err)
	}
}

type LabelsConfig struct {
	Major string `yaml:"major,omitempty"`
	Minor string `yaml:"minor,omitempty"`
}

type Strategy struct {
	Type string `yaml:"type,omitempty" validate:"oneof=pre-release full-release"`
}

type Config struct {
	TargetBranch string       `yaml:"targetBranch,omitempty" validate:"required"`
	Labels       LabelsConfig `yaml:"labels,omitempty"`
	Strategy     Strategy     `yaml:"strategy,omitempty"`
}

func getConfig(l echo.Logger, client scm.Client, repo Repo, ref string) (*Config, error) {
	config := &Config{
		TargetBranch: repo.GetDefaultBranch(),
		Labels: LabelsConfig{
			Major: "major",
			Minor: "minor",
		},
		Strategy: Strategy{
			Type: "pre-release",
		},
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
	if err := validate.Struct(config); err != nil {
		return nil, errors.Wrap(err, "Could not validate configuration")
	}
	return config, nil
}
