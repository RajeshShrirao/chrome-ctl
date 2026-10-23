package daemon

import (
	"context"
	"log"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
)

type Config struct {
	Addr           string
	Version        string
	Paths          config.Paths
	Log            *log.Logger
	ReadTimeout    time.Duration
	ForwardTimeout time.Duration
}

func Run(ctx context.Context, cfg Config) error {
	return serve(ctx, cfg)
}
