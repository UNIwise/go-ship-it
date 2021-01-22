package pkg

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/labstack/gommon/random"
	"github.com/pkg/errors"
)

type Server interface {
	Serve() error
}

type ServerImpl struct {
	AppID          int64
	PrivateKeyFile string
	GithubSecret   []byte
	Port           int32
	LogLevel       log.Lvl
}

func (s *ServerImpl) Serve() error {
	atr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, s.AppID, s.PrivateKeyFile)
	if err != nil {
		return errors.Wrap(err, "Error creating github app client")
	}

	handler := NewHandler(atr, s.GithubSecret)

	e := echo.New()

	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Skipper: middleware.DefaultSkipper,
		Generator: func() string {
			return random.String(8)
		},
	}))

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id := c.Request().Header.Get(echo.HeaderXRequestID)
			if id == "" {
				id = c.Response().Header().Get(echo.HeaderXRequestID)
			}
			logger := log.New(id)
			logger.SetLevel(s.LogLevel)
			logger.SetHeader(fmt.Sprintf("${level} %s ${prefix} [${short_file}:${line}]", id))
			c.SetLogger(logger)
			return next(c)
		}
	})

	e.POST("/github", handler.HandleGithub)
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ready to receive")
	})
	return e.Start(fmt.Sprintf(":%d", s.Port))
}