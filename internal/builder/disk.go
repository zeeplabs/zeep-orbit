package builder

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Disk limits on the shared work volume. Build steps run npm and vite, which
// write node_modules/, dist/ and the shared npm cache with no size bound of
// their own, and every session shares one volume: without these checks one
// session (a huge dependency tree, a vite config that writes in a loop) can
// fill it and break init, commit and build for every session.
//
// What this does NOT give is a hard quota. Sizes are measured by walking the
// tree, so a step can overshoot the cap by whatever it writes in one
// DiskCheckInterval, and code running in a step can write outside its session
// root (anywhere under the work dir, the npm cache, /tmp) because all sessions
// run as the same user. A hard per-session limit needs a filesystem quota or a
// volume per session; see the isolation notes in docs/docs/ai-frontend-builder.md.

// errSizeOver stops a directory walk as soon as the running total passes the
// limit.
var errSizeOver = errors.New("size limit exceeded")

// dirSizeOver walks dir without following symlinks and adds up the apparent
// size of regular files. It stops as soon as the total exceeds limit and then
// reports (total so far, true). Entries that vanish mid-walk (npm moves files
// around while it installs) and unreadable entries are skipped: the check is
// best effort and must never fail a step on its own.
func dirSizeOver(dir string, limit int64) (int64, bool) {
	if limit <= 0 {
		return 0, false
	}
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		if total > limit {
			return errSizeOver
		}
		return nil
	})
	return total, errors.Is(err, errSizeOver)
}

// reclaimBuildOutputs removes node_modules/ and dist/ from a session root.
// os.RemoveAll does not follow a symlink planted at either name; it removes
// the link itself.
func reclaimBuildOutputs(root string) {
	for _, name := range []string{"node_modules", distDirName} {
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

// watchDisk measures root every DiskCheckInterval while the step whose
// process group is pgid runs, and kills the group once the session passes
// MaxSessionDiskBytes. The caller reports the overrun after the step (the
// size is measured again there), so the watcher only has to stop the writes.
// The returned func stops the watcher and waits for it to exit.
func (e *Executor) watchDisk(root string, pgid int) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(e.cfg.DiskCheckInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if _, over := dirSizeOver(root, e.cfg.MaxSessionDiskBytes); over {
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			wg.Wait()
		})
	}
}

// pruneNpmCacheIfOver empties the shared npm cache when it has grown past
// MaxNpmCacheBytes. It never waits: if any step is running (and may be using
// the cache) it does nothing, and the next step to finish tries again. The
// cache directory itself is kept; only its entries are removed.
func (e *Executor) pruneNpmCacheIfOver() {
	cache := e.cfg.NpmCache
	if cache == "" || !filepath.IsAbs(cache) {
		return
	}
	if _, over := dirSizeOver(cache, e.cfg.MaxNpmCacheBytes); !over {
		return
	}
	if !e.cacheMu.TryLock() {
		return
	}
	defer e.cacheMu.Unlock()
	li, err := os.Lstat(cache)
	if err != nil || li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		return
	}
	for _, en := range entries {
		_ = os.RemoveAll(filepath.Join(cache, en.Name()))
	}
}
