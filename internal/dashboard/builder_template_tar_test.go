package dashboard

// builder_template_tar_test.go — pure unit tests (no database) of the template
// archive rewrite used by builder session creation (ai-frontend-builder T25).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"testing"
)

func bhPlainTar(t *testing.T, entries []tar.Header, bodies map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i := range entries {
		h := entries[i]
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(bodies[h.Name]))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("write header %s: %v", h.Name, err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(bodies[h.Name])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func drainTemplateTar(t *testing.T, tt *templateTar) (map[string]string, error) {
	t.Helper()
	files, rerr := bhReadTar(tt)
	if err := tt.Err(); err != nil {
		return files, err
	}
	return files, rerr
}

func TestTemplateTar_StripsTopDirReplacesConfigAndReadsVersion(t *testing.T) {
	src := bhPlainTar(t, []tar.Header{
		{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "x"}},
		{Typeflag: tar.TypeDir, Name: "acme-app-0123abc/", Mode: 0o755},
		{Typeflag: tar.TypeReg, Name: "acme-app-0123abc/package.json", Mode: 0o644},
		{Typeflag: tar.TypeDir, Name: "acme-app-0123abc/src/", Mode: 0o755},
		{Typeflag: tar.TypeReg, Name: "acme-app-0123abc/src/orbit.config.ts", Mode: 0o644},
		{Typeflag: tar.TypeReg, Name: "acme-app-0123abc/run.sh", Mode: 0o755},
	}, map[string]string{
		"acme-app-0123abc/package.json":        "{}",
		"acme-app-0123abc/src/orbit.config.ts": "OLD",
		"acme-app-0123abc/run.sh":              "#!/bin/sh",
	})
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = io.Copy(zw, src)
	_ = zw.Close()

	tt, err := newTemplateTar(&gz, true, true, builderConfigPath, []byte("NEW"))
	if err != nil {
		t.Fatalf("newTemplateTar: %v", err)
	}
	defer tt.Close()
	if tt.Version != "0123abc" {
		t.Errorf("version = %q, want 0123abc", tt.Version)
	}
	files, err := drainTemplateTar(t, tt)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	want := map[string]string{"package.json": "{}", "src/orbit.config.ts": "NEW", "run.sh": "#!/bin/sh"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for k, v := range want {
		if files[k] != v {
			t.Errorf("%s = %q, want %q", k, files[k], v)
		}
	}
}

func TestTemplateTar_AddsConfigWhenAbsentAndKeepsPlainTarLayout(t *testing.T) {
	src := bhPlainTar(t, []tar.Header{{Typeflag: tar.TypeReg, Name: "a.txt", Mode: 0o644}}, map[string]string{"a.txt": "A"})
	tt, err := newTemplateTar(src, false, false, builderConfigPath, []byte("CFG"))
	if err != nil {
		t.Fatal(err)
	}
	defer tt.Close()
	files, err := drainTemplateTar(t, tt)
	if err != nil || files["a.txt"] != "A" || files[builderConfigPath] != "CFG" || len(files) != 2 {
		t.Fatalf("files = %v err = %v", files, err)
	}
	if tt.Version != "" {
		t.Errorf("no prefix, no version; got %q", tt.Version)
	}
}

func TestTemplateTar_RejectsUnusableArchives(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		src := bhPlainTar(t, []tar.Header{
			{Typeflag: tar.TypeDir, Name: "top-1234567/", Mode: 0o755},
			{Typeflag: tar.TypeSymlink, Name: "top-1234567/link", Linkname: "/etc/passwd"},
		}, nil)
		tt, err := newTemplateTar(src, false, true, builderConfigPath, []byte("x"))
		if err != nil {
			t.Fatalf("the first entry is fine: %v", err)
		}
		defer tt.Close()
		if _, err := drainTemplateTar(t, tt); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
	t.Run("entry outside the top directory", func(t *testing.T) {
		src := bhPlainTar(t, []tar.Header{
			{Typeflag: tar.TypeDir, Name: "top-1234567/", Mode: 0o755},
			{Typeflag: tar.TypeReg, Name: "elsewhere/file", Mode: 0o644},
		}, map[string]string{"elsewhere/file": "x"})
		tt, err := newTemplateTar(src, false, true, builderConfigPath, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		defer tt.Close()
		if _, err := drainTemplateTar(t, tt); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
	t.Run("path traversal", func(t *testing.T) {
		src := bhPlainTar(t, []tar.Header{
			{Typeflag: tar.TypeReg, Name: "../evil", Mode: 0o644},
		}, map[string]string{"../evil": "x"})
		tt, err := newTemplateTar(src, false, false, builderConfigPath, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		defer tt.Close()
		if _, err := drainTemplateTar(t, tt); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
	t.Run("not a tar", func(t *testing.T) {
		if _, err := newTemplateTar(strings.NewReader(strings.Repeat("x", 2048)), false, false, builderConfigPath, nil); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if _, err := newTemplateTar(bhPlainTar(t, nil, nil), false, false, builderConfigPath, nil); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
	t.Run("not gzip", func(t *testing.T) {
		if _, err := newTemplateTar(strings.NewReader("plain text"), true, true, builderConfigPath, nil); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
	t.Run("file at the root when stripping", func(t *testing.T) {
		src := bhPlainTar(t, []tar.Header{{Typeflag: tar.TypeReg, Name: "loose.txt", Mode: 0o644}}, map[string]string{"loose.txt": "x"})
		if _, err := newTemplateTar(src, false, true, builderConfigPath, nil); !errors.Is(err, errTemplateUnusable) {
			t.Fatalf("want errTemplateUnusable, got %v", err)
		}
	})
}

// Closing early (the builder rejected the stream) stops the producer instead of
// leaking its goroutine, and is not reported as an archive problem.
func TestTemplateTar_CloseEarlyStopsProducer(t *testing.T) {
	entries := []tar.Header{{Typeflag: tar.TypeDir, Name: "top-abcdef0/", Mode: 0o755}}
	bodies := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d"} {
		name := "top-abcdef0/" + n
		entries = append(entries, tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644})
		bodies[name] = strings.Repeat(n, 64<<10)
	}
	tt, err := newTemplateTar(bhPlainTar(t, entries, bodies), false, true, builderConfigPath, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	if _, err := io.ReadFull(tt, buf); err != nil {
		t.Fatal(err)
	}
	tt.Close() // must return, not hang
	if err := tt.Err(); err != nil {
		t.Errorf("an early close is not an archive error, got %v", err)
	}
}

func TestCapReaderFailsInsteadOfTruncating(t *testing.T) {
	cr := &capReader{r: strings.NewReader(strings.Repeat("x", 100)), left: 10}
	_, err := io.ReadAll(cr)
	if !errors.Is(err, errTemplateUnusable) {
		t.Fatalf("want errTemplateUnusable at the cap, got %v", err)
	}
}

func TestVersionFromTarballPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"acme-starter-1a2b3c4":                                  "1a2b3c4",
		"acme-starter-1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d": "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d",
		"acme-starter":                                          "",
		"acme-starter-main":                                     "",
		"acme-starter-1A2B3C4":                                  "",
		"acme-starter-123":                                      "",
		"nohyphen":                                              "",
	} {
		if got := versionFromTarballPrefix(in); got != want {
			t.Errorf("versionFromTarballPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderOrbitConfigEscapesValues(t *testing.T) {
	got := string(renderOrbitConfig("https://x.example/it's", "a'b\\c\n\u2028"))
	for _, want := range []string{
		`export const ORBIT_API_URL: string = 'https://x.example/it\'s'`,
		`export const ORBIT_APP_NAME: string = 'a\'b\\c\u000a\u2028'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n") != 5 {
		t.Errorf("a value must not be able to add lines:\n%s", got)
	}
}
