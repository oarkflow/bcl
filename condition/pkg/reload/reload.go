package reload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oarkflow/bcl"
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
	root, err := w.rootPath(ctx, req)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("reload path %q is a directory", root)
	}

	debounce := time.Duration(req.DebounceMillis) * time.Millisecond
	if debounce <= 0 {
		debounce = w.Interval
	}
	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}
	watchReq := req
	watchReq.Path = root
	includeImports := watchReq.IncludeImports == nil || *watchReq.IncludeImports
	opts := &bcl.Options{ResolveImports: includeImports, ResolveModules: true, BaseDir: filepath.Dir(root)}

	events := make(chan bcl.WatchEvent, 16)
	var stop chan struct{}
	if includeImports {
		stop = bcl.WatchFilesWithDependencies([]string{root}, opts, func(event bcl.WatchEvent) {
			select {
			case events <- event:
			default:
			}
		})
	} else {
		stop = bcl.WatchFiles([]string{root}, opts, func(event bcl.WatchEvent) {
			select {
			case events <- event:
			default:
			}
		})
	}
	if stop != nil {
		defer close(stop)
	}
	snapshot := snapshotBCLFiles(filepath.Dir(root))

	var (
		mu      sync.Mutex
		timer   *time.Timer
		pending bcl.WatchEvent
	)
	fire := func(event bcl.WatchEvent) {
		changed, nextSnapshot := changedBCLPath(snapshotBCLFiles(filepath.Dir(root)), snapshot)
		if changed != "" {
			event.Path = changed
			event.Dependency = changed
		}
		snapshot = nextSnapshot
		reloadReq := watchReq
		resp, _ := w.Service.Reload(ctx, reloadReq)
		if resp != nil {
			resp.ChangedPath = firstNonEmpty(event.Path, event.Dependency, root)
			resp.DependencyPath = firstNonEmpty(event.Dependency, event.Path, root)
		}
		if onReload != nil {
			onReload(resp)
		}
	}
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			if timer != nil {
				timer.Stop()
			}
			mu.Unlock()
			return ctx.Err()
		case event := <-events:
			mu.Lock()
			pending = event
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				mu.Lock()
				event := pending
				mu.Unlock()
				fire(event)
			})
			mu.Unlock()
		}
	}
}

func (w Watcher) rootPath(ctx context.Context, req condition.ReloadRequest) (string, error) {
	if req.Path != "" {
		return req.Path, nil
	}
	if req.Name == "" {
		return "", fmt.Errorf("reload watch requires definition name or path")
	}
	record, err := w.Service.GetDefinition(ctx, req.Name)
	if err != nil {
		return "", err
	}
	if record.SourcePath == "" {
		return "", fmt.Errorf("definition %q was published from source and cannot be file-watched", req.Name)
	}
	return record.SourcePath, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func snapshotBCLFiles(root string) map[string]time.Time {
	out := map[string]time.Time{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".bcl" {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return nil
		}
		out[path] = info.ModTime()
		return nil
	})
	return out
}

func changedBCLPath(current, previous map[string]time.Time) (string, map[string]time.Time) {
	var changed string
	for path, mod := range current {
		if previous[path].IsZero() || mod.After(previous[path]) {
			if changed == "" || path < changed {
				changed = path
			}
		}
	}
	if changed == "" {
		for path := range previous {
			if _, ok := current[path]; !ok {
				if changed == "" || path < changed {
					changed = path
				}
			}
		}
	}
	return changed, current
}
