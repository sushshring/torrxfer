package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
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

type barDetails struct {
	bar       *mpb.Bar
	startTime time.Time
	lastTime  time.Time
	once      sync.Once
	done      chan struct{}
}

func main() {
	app.Version(version)
	kingpin.MustParse(app.Parse(os.Args[1:]))
	var level zerolog.Level
	if *debug {
		level = zerolog.DebugLevel
	} else {
		level = zerolog.InfoLevel
	}
	common.ConfigureLogging(level, false, os.Stderr)

	log.Debug().Msg("Starting the Torrxfer client")

	client := torrxfer.NewTorrxferClient()
	err := client.Run(*config)

	if err != nil {
		log.Info().Err(err).Msg("Failed to launch client")
		os.Exit(-1)
	}

	progressBarMap := make(map[string]*barDetails)
	p := mpb.New(nil)

	for notification := range client.RegisterForConnectionNotifications() {
		switch notification.NotificationType {
		case torrxfer.ConnectionNotificationTypeConnected:
			log.Info().Object("Server", notification.Connection).Msg("Connected to server")
		case torrxfer.ConnectionNotificationTypeDisconnected:
			log.Info().Object("Server", notification.Connection).Msg("Disconnected")
		case torrxfer.ConnectionNotificationTypeQueryError:
			fallthrough
		case torrxfer.ConnectionNotificationTypeTransferError:
			log.Error().Err(notification.Error).Object("Server", notification.Connection).Object("File", notification.SentFile).Msg("Error")
		case torrxfer.ConnectionNotificationTypeFilesUpdated:
			if progressBar, ok := progressBarMap[notification.SentFile.Path]; !ok {
				bar := p.Add(int64(notification.SentFile.Size),
					mpb.NewBarFiller("[=>-|"),
					mpb.PrependDecorators(
						decor.Name(fmt.Sprintf("Transferring file. Name: %s | ", filepath.Base(notification.SentFile.Path))),
						decor.CountersKiloByte("% .2f / % .2f"),
					),
					mpb.AppendDecorators(
						decor.EwmaETA(decor.ET_STYLE_GO, 60),
						decor.Name(" ] "),
						decor.EwmaSpeed(decor.UnitKiB, "% .2f ", 60),
						decor.OnComplete(
							// ETA decorator with ewma age of 60
							decor.Elapsed(decor.ET_STYLE_GO), "done",
						),
					),
				)
				bar.SetCurrent(int64(notification.Connection.GetFileSizeOnServer(notification.SentFile.Path)))
				progressBarMap[notification.SentFile.Path] = &barDetails{
					bar:       bar,
					startTime: time.Time{},
					lastTime:  time.Time{},
					once:      sync.Once{},
					done:      make(chan struct{}),
				}
			} else {
				progressBar.bar.IncrInt64(int64(notification.LastSentSize))
				progressBar.bar.DecoratorEwmaUpdate(time.Since(progressBar.lastTime))
				progressBar.lastTime = time.Now()
			}
			log.Info().Object("Server", notification.Connection)
		}
	}
}
