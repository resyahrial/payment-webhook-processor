package logging

import (
	"io"

	"github.com/rs/zerolog"
)

func NewJSONLogger(writer io.Writer) *zerolog.Logger {
	logger := zerolog.New(writer).With().Timestamp().Logger()
	return &logger
}
