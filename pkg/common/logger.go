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

// LogFileDecoder wraps a writer and writes to it
type LogFileDecoder struct {
	Writer io.Writer
}

var loggers map[io.Writer]bool

// Decode takes a file name and opens it as the log file
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

// LogErrorStack wrapper around logging an error with the current stack
func LogErrorStack(err error, message string) {
	log.Error().Stack().Err(err).Msg(message)
}

// LogError wrapper around logging regular error
func LogError(err error, message string) {
	log.Error().Err(err).Msg(message)
}

// ConfigureLogging configures all the log related settings for the application
func ConfigureLogging(debug bool, shortLog bool, loggers ...io.Writer) {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	for _, logger := range loggers {
		AddLogger(logger, shortLog)
	}
}

// AddLogger adds a new logger to the application and logs to all active loggers
func AddLogger(logger io.Writer, shortLog bool) {
	if logger == nil {
		logger = os.Stdout
	}
	if ok := loggers[logger]; !ok {
		loggers[logger] = true
	}

	multis := make([]io.Writer, 0, len(loggers))

	for l := range loggers {
		writer := zerolog.ConsoleWriter{Out: l, TimeFormat: time.RFC1123}
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
		multis = append(multis, l)
	}

	multi := zerolog.MultiLevelWriter(multis...)
	log.Logger = zerolog.New(multi).With().Caller().Stack().Logger()
}
