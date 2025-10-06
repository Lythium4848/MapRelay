package logging

import (
	"os"

	"go.uber.org/zap"
)

var base *zap.Logger

func Init() {
	var l *zap.Logger

	if os.Getenv("APP_ENV") == "development" {
		l = zap.Must(zap.NewDevelopment())
	} else {
		l = zap.Must(zap.NewProduction())
	}

	zap.ReplaceGlobals(l)

	base = l
}

func Sync() error {
	if base != nil {
		return base.Sync()
	}

	return nil
}

func L() *zap.Logger {
	if base == nil {
		Init()
	}

	return base
}

func Named(name string) *zap.Logger {
	return L().Named(name)
}
