package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func Init(level zerolog.Level) {

	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.Kitchen, // e.g. 3:04PM
		NoColor:    false,        // force colors (set true if piping)
	}

	log.Logger = zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Logger()
}

func CliInit(ctx context.Context, c *cli.Command) (context.Context, error) {
	level := c.Int("log-level")
	switch level {
	case 0:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case 1:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case 2:
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case 3:
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case 4:
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case 5:
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	log.Debug().Int("level", level).Msg("Log level set")
	return ctx, nil
}
