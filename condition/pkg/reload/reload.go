package reload

import (
	"context"
	"os"
	"time"

	condition "github.com/oarkflow/condition/pkg/condition"
)

type Watcher struct {
	Service  *condition.Service
	Interval time.Duration
}

func (w Watcher) Watch(ctx context.Context, req condition.ReloadRequest, onReload func(*condition.ReloadResponse)) error {
	if w.Service == nil {
		return nil
	}
	interval := w.Interval
	if interval == 0 {
		interval = time.Second
	}
	info, err := os.Stat(req.Path)
	if err != nil {
		return err
	}
	last := info.ModTime()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			info, err := os.Stat(req.Path)
			if err != nil {
				continue
			}
			if !info.ModTime().After(last) {
				continue
			}
			last = info.ModTime()
			resp, _ := w.Service.Reload(ctx, req)
			if onReload != nil {
				onReload(resp)
			}
		}
	}
}
