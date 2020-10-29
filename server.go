package main

import (
	"net/http"
	"os"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"
	"github.com/uniwise/git-releaser/pkg"
)

func main() {
	godotenv.Load(".env")

	appid, err := strconv.Atoi(os.Getenv("GITHUB_APP_ID"))
	if err != nil {
		logrus.Fatal(err)
	}
	atr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, int64(appid), os.Getenv("GITHUB_CERT_PATH"))
	if err != nil {
		logrus.Fatal("error creating github app client", err)
	}

	handler := pkg.NewHandler(atr, []byte(os.Getenv("GITHUB_SECRET")))

	e := echo.New()
	e.Use(middleware.Logger())
	e.POST("/github", handler.HandleGithub)
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ready to receive")
	})
	e.Logger.Fatal(e.Start(":1323"))
}
