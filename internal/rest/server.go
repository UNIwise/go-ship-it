package rest

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/sirupsen/logrus"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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
	Logger         *logrus.Entry
}

func (s *ServerImpl) Serve() error {
	atr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, s.AppID, s.PrivateKeyFile)
	if err != nil {
		return errors.Wrap(err, "Error creating github app client")
	}

	handler := NewHandler(atr, s.GithubSecret, s.Logger.WithField("subsystem", "handler"))

	e := echo.New()
	e.Use(middleware.Recover())

	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Skipper: middleware.DefaultSkipper,
		Generator: func() string {
			return random.String(10, random.Hex)
		},
	}))

	// e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
	// 	return func(c echo.Context) error {
	// 		id := c.Response().Header().Get(echo.HeaderXRequestID)
	// 		logger := log.New(id)
	// 		logger.SetLevel(s.LogLevel)
	// 		logger.SetHeader(fmt.Sprintf("${level}\t%s\t${prefix}\t[${short_file}:${line}]\t", color.HEX(id[0:6]).Sprint(id)))
	// 		c.SetLogger(logger)
	// 		return next(c)
	// 	}
	// })

	e.POST("/github", handler.HandleGithub)
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ready to receive")
	})
	return e.Start(fmt.Sprintf(":%d", s.Port))
}
