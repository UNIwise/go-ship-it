package rest

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/random"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "github.com/uniwise/go-ship-it/internal/rest/v1"
)

type Server interface {
	Serve() error
}

type ServerImpl struct {
	AppID          int64
	PrivateKeyFile string
	GithubSecret   []byte
	Port           int32
	Logger         *logrus.Entry
}

func (s *ServerImpl) Serve() error {
	atr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, s.AppID, s.PrivateKeyFile)
	if err != nil {
		return errors.Wrap(err, "Error creating github app client")
	}

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Skipper: middleware.DefaultSkipper,
		Generator: func() string {
			return random.String(10, random.Hex)
		},
	}))

	g := e.Group("/v1")
	v1.Register(g, atr, s.GithubSecret, s.Logger.WithField("subsystem", "handler"))
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ready to receive")
	})

	return e.Start(fmt.Sprintf(":%d", s.Port))
}
