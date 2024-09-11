package signal

import (
	"context"
	"os"
	"os/signal"
)

func NotifyContext(ctx context.Context, sig ...os.Signal) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, sig...)

	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
		<-sigCh
		os.Exit(1)
	}()

	return ctx, func() {
		signal.Stop(sigCh)
		cancel()
	}
}
