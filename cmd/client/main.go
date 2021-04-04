package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog/log"
	torrxfer "github.com/sushshring/torrxfer/pkg/client"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/vbauerster/mpb/v6"
	"github.com/vbauerster/mpb/v6/decor"
)

var (
	name    = "torrxfer-client"
	app     = kingpin.New(name, "Torrent downloaded file transfer server")
	debug   = app.Flag("debug", "Enable debug mode").Default("false").OverrideDefaultFromEnvar("TORRXFER_CLIENT_DEBUG").Bool()
	config  = app.Flag("config", "Path to configuration file").Required().File()
	version = "0.1"
)

func main() {
	app.Version(version)
	kingpin.MustParse(app.Parse(os.Args[1:]))
	common.ConfigureLogging(*debug, false, nil)

	log.Debug().Msg("Starting the Torrxfer client")

	client := torrxfer.NewTorrxferClient()
	err := client.Run(*config)

	progressBarMap := make(map[string]*mpb.Bar)
	p := mpb.New(nil)

	for notification := range client.RegisterForConnectionNotifications() {
		switch notification.NotificationType {
		case torrxfer.ConnectionNotificationTypeConnected:
			log.Info().Object("Server", notification.Connection).Msg("Connected to server")
		case torrxfer.ConnectionNotificationTypeDisconnected:
			log.Info().Object("Server", notification.Connection).Msg("Disconnected")
		case torrxfer.ConnectionNotificationTypeQueryError:
			log.Info().Object("Server", notification.Connection).Object("File", notification.SentFile).Msg("Query Error")
		case torrxfer.ConnectionNotificationTypeFilesUpdated:
			if progressBar, ok := progressBarMap[notification.SentFile.Path]; !ok {
				bar := p.Add(int64(notification.SentFile.Size),
					mpb.NewBarFiller("[=>-|"),
					mpb.PrependDecorators(
						decor.Name(fmt.Sprintf("Transferring file. Path: %s", notification.SentFile.Path))
						decor.CountersKibiByte("% .2f / % .2f"),
					),
					mpb.AppendDecorators(
						decor.EwmaETA(decor.ET_STYLE_GO, 90),
						decor.Name(" ] "),
						decor.EwmaSpeed(decor.UnitKiB, "% .2f", 60),
					),
				)
				progressBarMap[notification.SentFile.Path] = bar
			} else {
				progressBar.IncrBy(int(notification.Connection.GetFileSizeOnServer(notification.SentFile.Path)))
			}
			log.Info().Object("Server", notification.Connection)
		}
	}
	if err != nil {
		os.Exit(-1)
	}
	// internal.StartUI(client)
}
