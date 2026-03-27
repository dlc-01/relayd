package logger

import (
	"go.uber.org/zap"
)

func New(component string) *zap.SugaredLogger {
	base, _ := zap.NewProduction()
	return base.Sugar().With("component", component)
}

func NewDev(component string) *zap.SugaredLogger {
	base, _ := zap.NewDevelopment()
	return base.Sugar().With("component", component)
}
