package logging

import (
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Init configures the global logger to write JSON to a rotating file in
// logDir/mosaic.log and pretty console output to stderr in debug mode.
// Returns a closer that flushes the file writer.
func Init(logDir string, debug bool) (io.Closer, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}

	rot := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "mosaic.log"),
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     14, // days
		Compress:   true,
	}

	var writers []io.Writer = []io.Writer{rot}
	if debug {
		writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr})
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	level := zerolog.InfoLevel
	if debug {
		level = zerolog.DebugLevel
	}
	log.Logger = zerolog.New(io.MultiWriter(writers...)).
		Level(level).
		With().
		Timestamp().
		Logger()

	log.Info().Str("log_dir", logDir).Msg("logging initialized")
	return rot, nil
}
