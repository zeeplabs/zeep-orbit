package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const mib = 1 << 20

func writeSized(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDirSizeOver(t *testing.T) {
	dir := t.TempDir()
	writeSized(t, filepath.Join(dir, "a"), 600<<10)
	writeSized(t, filepath.Join(dir, "sub", "b"), 600<<10)
	// A symlink to a big file outside must not be followed or counted.
	outside := t.TempDir()
	writeSized(t, filepath.Join(outside, "huge"), 4*mib)
	if err := os.Symlink(filepath.Join(outside, "huge"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}

	if _, over := dirSizeOver(dir, 2*mib); over {
		t.Fatal("1.2 MiB of files reported over a 2 MiB limit (symlink followed?)")
	}
	if size, over := dirSizeOver(dir, 1*mib); !over || size <= mib {
		t.Fatalf("want over a 1 MiB limit, got size=%d over=%v", size, over)
	}
	if _, over := dirSizeOver(filepath.Join(dir, "missing"), 1); over {
		t.Fatal("missing directory reported over")
	}
	if _, over := dirSizeOver(dir, 0); over {
		t.Fatal("limit 0 must disable the check")
	}
}

func TestExecSessionOverDiskLimitFailsAndReclaims(t *testing.T) {
	// The fake "npm ci/install" fills node_modules past the cap and exits 0.
	body := `mkdir -p node_modules/pkg && dd if=/dev/zero of=node_modules/pkg/blob bs=1024 count=2048 2>/dev/null; mkdir -p dist; echo ok`
	f := newExecFixture(t, body, body, func(c *ExecConfig) {
		c.MaxSessionDiskBytes = 1 * mib
		c.DiskCheckInterval = time.Hour // only the post-step check runs
	})
	writeSized(t, filepath.Join(f.root, "src", "main.ts"), 100)

	res, err := f.ex.Run(context.Background(), f.root, StepInstall, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("step over the disk limit reported success: %+v", res)
	}
	if !strings.Contains(res.OutputTail, "disk usage passed the 1 MiB limit") {
		t.Fatalf("tail lacks the disk limit note: %q", res.OutputTail)
	}
	for _, name := range []string{"node_modules", "dist"} {
		if _, err := os.Lstat(filepath.Join(f.root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s was not removed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, "src", "main.ts")); err != nil {
		t.Errorf("agent file was removed: %v", err)
	}

	t.Run("under the limit is untouched", func(t *testing.T) {
		g := newExecFixture(t, `mkdir -p node_modules && echo x > node_modules/f`, `true`, func(c *ExecConfig) {
			c.MaxSessionDiskBytes = 1 * mib
		})
		res, err := g.ex.Run(context.Background(), g.root, StepInstall, "")
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("got %+v, %v", res, err)
		}
		if _, err := os.Stat(filepath.Join(g.root, "node_modules", "f")); err != nil {
			t.Fatalf("node_modules removed under the limit: %v", err)
		}
	})
}

func TestExecDiskWatchdogKillsRunawayStep(t *testing.T) {
	body := `dd if=/dev/zero of=runaway.bin bs=1024 count=2048 2>/dev/null; sleep 30`
	f := newExecFixture(t, body, body, func(c *ExecConfig) {
		c.MaxSessionDiskBytes = 1 * mib
		c.DiskCheckInterval = 50 * time.Millisecond
		c.Timeouts.Lint = 20 * time.Second
	})
	start := time.Now()
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("runaway step was not killed by the disk watchdog: %v", elapsed)
	}
	if res.TimedOut || res.ExitCode == 0 {
		t.Fatalf("want a failed, non-timeout result, got %+v", res)
	}
	if !strings.Contains(res.OutputTail, "disk usage passed") {
		t.Fatalf("tail lacks the disk limit note: %q", res.OutputTail)
	}
}

func TestNpmCachePrunedWhenOverLimit(t *testing.T) {
	f := newExecFixture(t, `true`, `true`, func(c *ExecConfig) {
		c.MaxNpmCacheBytes = 1 * mib
	})
	cache := f.ex.cfg.NpmCache
	writeSized(t, filepath.Join(cache, "_cacache", "content", "blob"), 2*mib)

	// While a step holds the cache, the prune must not run.
	f.ex.cacheMu.RLock()
	f.ex.pruneNpmCacheIfOver()
	f.ex.cacheMu.RUnlock()
	if _, err := os.Stat(filepath.Join(cache, "_cacache", "content", "blob")); err != nil {
		t.Fatalf("cache pruned while in use: %v", err)
	}

	if _, err := f.ex.Run(context.Background(), f.root, StepLint, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatalf("cache directory must survive the prune: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache not emptied: %d entries left", len(entries))
	}

	t.Run("under the limit is kept", func(t *testing.T) {
		g := newExecFixture(t, `true`, `true`, func(c *ExecConfig) { c.MaxNpmCacheBytes = 1 * mib })
		p := filepath.Join(g.ex.cfg.NpmCache, "index", "x")
		writeSized(t, p, 1024)
		if _, err := g.ex.Run(context.Background(), g.root, StepLint, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("cache under the limit was pruned: %v", err)
		}
	})
}
