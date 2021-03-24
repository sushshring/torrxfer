package common

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

type LogFileDecoder struct {
	Writer io.Writer
}

func (d *LogFileDecoder) Decode(value string) error {
	if value == "" {
		d.Writer = nil
		return nil
	}
	file, err := os.Open(value)
	if err != nil {
		return err
	}
	d.Writer = file
	return nil
}

func LogErrorStack(err error, message string) {
	log.Error().Stack().Err(err).Msg(message)
}

func LogError(err error, message string) {
	log.Error().Err(err).Msg(message)
}

// ConfigureLogging configures all the log related settings for the application
func ConfigureLogging(debug bool, logger io.Writer, shortLog bool) {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	ChangeLogger(logger, shortLog)
}

func ChangeLogger(logger io.Writer, shortLog bool) {
	if logger == nil {
		logger = os.Stdout
	}
	writer := zerolog.ConsoleWriter{Out: logger, TimeFormat: time.RFC1123}
	if shortLog {
		writer.FormatCaller = func(i interface{}) string {
			return ""
		}
		writer.FormatFieldName = func(i interface{}) string {
			return fmt.Sprintf("%s:", i)
		}
		writer.FormatFieldValue = func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("%s", i))
		}
		writer.FormatLevel = func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("| %s |", i))
		}
	}
	log.Logger = log.With().Caller().Stack().Logger().Output(writer)
	zerolog.ErrorStackMarshaler
}
