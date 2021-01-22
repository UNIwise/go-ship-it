/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"github.com/labstack/gommon/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uniwise/go-ship-it/pkg"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the server",
	Long:  `Start the go-ship-it server`,
	Run: func(cmd *cobra.Command, args []string) {
		lvl := log.INFO
		switch viper.GetString("server.loglevel") {
		case "debug":
			lvl = log.DEBUG
		case "warn":
			lvl = log.WARN
		case "error":
			lvl = log.ERROR
		case "off":
			lvl = log.OFF
		case "info":
		default:
			lvl = log.INFO
		}

		s := &pkg.ServerImpl{
			AppID:          viper.GetInt64("github.appid"),
			PrivateKeyFile: viper.GetString("github.keyfile"),
			GithubSecret:   []byte(viper.GetString("github.secret")),
			Port:           viper.GetInt32("server.port"),
			LogLevel:       lvl,
		}

		err := s.Serve()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	serveCmd.PersistentFlags().Int64("app-id", 0, "Github app id")
	serveCmd.PersistentFlags().String("key-file", "", "Key file containing private RSA key to authenticate against github")
	serveCmd.PersistentFlags().String("secret", "", "Github webhook secret")

	serveCmd.PersistentFlags().Int32("port", 80, "Port for the server to listen on")
	serveCmd.PersistentFlags().String("log-level", "", "The log level of the server")

	viper.BindPFlag("github.appid", serveCmd.PersistentFlags().Lookup("app-id"))
	viper.BindPFlag("github.keyfile", serveCmd.PersistentFlags().Lookup("key-file"))
	viper.BindPFlag("github.secret", serveCmd.PersistentFlags().Lookup("secret"))

	viper.BindPFlag("server.port", serveCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("server.loglevel", serveCmd.PersistentFlags().Lookup("log-level"))

	rootCmd.AddCommand(serveCmd)
}