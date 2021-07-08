package v1

import (
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

type Handler struct {
	Secret        []byte
	AppsTransport *ghinstallation.AppsTransport
}

func NewHandler(atr *ghinstallation.AppsTransport, secret []byte) *Handler {
	return &Handler{
		AppsTransport: atr,
		Secret:        secret,
	}
}

func Register(g *echo.Group, transport *ghinstallation.AppsTransport, secret []byte, l *logrus.Entry) {
	h := NewHandler(transport, secret)

	g.POST("/github", wrap(h.HandleGithub, l))
	g.File("/schema", "assets/schema/v1.json")
}

type handlerFunc func(echo.Context, *logrus.Entry) error

func wrap(handle handlerFunc, l *logrus.Entry) echo.HandlerFunc {
	return func(c echo.Context) error {
		entry := l.WithFields(logrus.Fields{
			"trace-id": c.Response().Header().Get(echo.HeaderXRequestID),
			"path":     c.Path(),
		})
		entry.Debug("handling")

		return handle(c, entry)
	}
}
