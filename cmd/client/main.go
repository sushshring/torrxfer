package main

import (
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog/log"
	torrxfer "github.com/sushshring/torrxfer/pkg/client"
	"github.com/sushshring/torrxfer/pkg/common"
)

var (
	name    = "torrxfer-client"
	app     = kingpin.New(name, "Torrent downloaded file transfer server")
	debug   = app.Flag("debug", "Enable debug mode").Default("false").Bool()
	config  = app.Flag("config", "Path to configuration file").Required().File()
	version = "0.1"
)

func main() {
	app.Version(version)
	kingpin.MustParse(app.Parse(os.Args[1:]))
	common.ConfigureLogging(*debug, nil, false)

	log.Debug().Msg("Starting the Torrxfer client")

	client := torrxfer.NewTorrxferClient()
	err := client.Run(*config)
	if err != nil {
		os.Exit(-1)
	}
	// internal.StartUI(client)
}
