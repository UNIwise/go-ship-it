package main

import (
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"
	"github.com/uniwise/git-releaser/pkg"
)

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		logrus.Fatal(err)
	}
	handler := pkg.NewHandler([]byte(os.Getenv("SECRET")))

	e := echo.New()
	e.Use(middleware.Logger())
	e.POST("/github", handler.HandleGithub)
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ready to receive")
	})
	e.Logger.Fatal(e.Start(":1323"))
}
