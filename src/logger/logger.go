package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
