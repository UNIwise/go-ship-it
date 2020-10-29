package main

import (
	"context"
	"net/http"
	"os"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v32/github"
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

	appid, err := strconv.Atoi(os.Getenv("GITHUB_APP_ID"))
	if err != nil {
		logrus.Fatal(err)
	}
	atr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, int64(appid), os.Getenv("GITHUB_CERT_PATH"))
	if err != nil {
		logrus.Fatal("error creating github app client", err)
	}

	var installation *github.Installation
	if user, ok := os.LookupEnv("GITHUB_USER"); ok {
		installation, _, err = github.NewClient(&http.Client{Transport: atr}).Apps.FindUserInstallation(context.TODO(), user)
	}
	if org, ok := os.LookupEnv("GITHUB_ORG"); ok {
		installation, _, err = github.NewClient(&http.Client{Transport: atr}).Apps.FindOrganizationInstallation(context.TODO(), org)
	}
	if err != nil || installation == nil {
		logrus.Fatalf("error finding organization/user installation: %v", err)
	}

	installationID := installation.GetID()
	itr := ghinstallation.NewFromAppsTransport(atr, installationID)

	logrus.Printf("successfully initialized GitHub app client, installation-id:%s expected-events:%v\n")

	client := pkg.NewClient(&http.Client{Transport: itr})
	handler := pkg.NewHandler(client, []byte(os.Getenv("GITHUB_SECRET")))

	e := echo.New()
	e.Use(middleware.Logger())
	e.POST("/github", handler.HandleGithub)
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ready to receive")
	})
	e.Logger.Fatal(e.Start(":1323"))
}
