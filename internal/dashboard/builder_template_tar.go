package dashboard

// builder_template_tar.go — turns a template archive into the plain tar the
// builder's Init accepts, with the generated src/orbit.config.ts put in
// (ai-frontend-builder T25).
//
// Why Orbit rewrites the tar instead of writing the file after Init: Init makes
// the "turn 0" commit and returns its full bundle, which is the only snapshot
// that can restore a workspace on its own. A file written afterwards would sit
// outside that commit, so a restore (volume loss) or a revert to turn 0 would
// bring the placeholder config back. Putting the file in the tree Init commits
// keeps turn 0 self-contained with one bundle.
//
// Inputs:
//   - a company template: the gzip tarball GitHub serves, whose single
//     top-level directory ("<owner>-<repo>-<sha>/") is stripped; the short sha
//     becomes the template version;
//   - the default template: the plain tar the builder produced from its own
//     copy (git archive), already rooted at the repository top.
//
// Only regular files and directories are copied. Anything else (symlinks,
// hardlinks, devices) makes the template unusable; the builder would reject it
// too, but failing here gives one deterministic error. Sizes are bounded again
// by the builder's own limits; the expanded-size cap here only stops a
// decompression bomb from being pumped at it.

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
)

const (
	// builderConfigPath is the file the template reads the Orbit connection
	// info from (builder/templates/default/src/orbit.config.ts).
	builderConfigPath = "src/orbit.config.ts"

	// maxTemplateExpandedBytes caps the decompressed tar stream read from a
	// template archive.
	maxTemplateExpandedBytes = 512 << 20
	// maxDefaultTemplateTarBytes caps the default template tar read back from
	// the builder before it is re-sent with the generated config.
	maxDefaultTemplateTarBytes = 64 << 20
)

// errTemplateUnusable marks a template archive that cannot become a workspace
// (not a tar, wrong layout, unsupported entry types, too large).
var errTemplateUnusable = errors.New("template archive is not usable")

var errTemplateTooLarge = fmt.Errorf("%w: expanded size limit exceeded", errTemplateUnusable)

// capReader fails (instead of returning EOF) once more than left bytes are
// read, so a truncated stream can never look like a complete archive.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errTemplateTooLarge
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// templateTar is a plain tar stream being produced in the background from a
// template archive. Read it, then Close it (always) and check Err.
type templateTar struct {
	pr   *io.PipeReader
	done chan struct{}
	once sync.Once
	err  error // written before done is closed

	// src, when set, is closed by Close after the producer has stopped.
	src io.Closer

	// Version is the template version derived from the archive ("" when the
	// archive does not carry one).
	Version string
}

// Read implements io.Reader.
func (t *templateTar) Read(p []byte) (int, error) { return t.pr.Read(p) }

// Close stops the producer and waits for it. Safe to call more than once.
func (t *templateTar) Close() {
	t.once.Do(func() {
		_ = t.pr.CloseWithError(io.ErrClosedPipe)
		<-t.done
		if t.src != nil {
			_ = t.src.Close()
		}
	})
}

// Err is the producer's failure, if any. Call it after Close: it is the
// reason the stream ended early (errTemplateUnusable wraps archive problems).
func (t *templateTar) Err() error {
	t.Close()
	if errors.Is(t.err, io.ErrClosedPipe) {
		return nil
	}
	return t.err
}

// newTemplateTar reads src (gzip-compressed when gz) and returns the plain tar
// of its contents, with overlayContent stored at overlayPath (replacing the
// file of that name, or added). When stripTop is set the archive's single
// top-level directory is removed from every path.
//
// The first real entry is read synchronously so that an unusable archive is
// reported here and Version is known before any session row exists; the rest
// is streamed.
func newTemplateTar(src io.Reader, gz, stripTop bool, overlayPath string, overlayContent []byte) (*templateTar, error) {
	r := src
	if gz {
		zr, err := gzip.NewReader(src)
		if err != nil {
			return nil, fmt.Errorf("%w: not a gzip stream: %v", errTemplateUnusable, err)
		}
		r = zr
	}
	tr := tar.NewReader(&capReader{r: r, left: maxTemplateExpandedBytes})

	var first *tar.Header
	for first == nil {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("%w: empty archive", errTemplateUnusable)
			}
			return nil, fmt.Errorf("%w: %v", errTemplateUnusable, err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		first = hdr
	}

	mp := &templateNameMapper{strip: stripTop}
	if stripTop {
		top, rest, _ := strings.Cut(strings.TrimPrefix(first.Name, "./"), "/")
		if top == "" || (rest == "" && first.Typeflag != tar.TypeDir) {
			return nil, fmt.Errorf("%w: expected one top-level directory", errTemplateUnusable)
		}
		mp.prefix = top
	}

	pr, pw := io.Pipe()
	t := &templateTar{pr: pr, done: make(chan struct{}), Version: versionFromTarballPrefix(mp.prefix)}
	go func() {
		defer close(t.done)
		err := pumpTemplateTar(tr, first, mp, pw, overlayPath, overlayContent)
		t.err = err
		_ = pw.CloseWithError(err) // nil closes with io.EOF
	}()
	return t, nil
}

// templateNameMapper maps archive entry names to workspace-relative paths.
type templateNameMapper struct {
	strip  bool
	prefix string
}

// rel returns the cleaned relative path of an entry ("" to skip it).
func (m *templateNameMapper) rel(name string) (string, error) {
	n := strings.TrimPrefix(name, "./")
	if m.strip {
		switch {
		case n == m.prefix:
			return "", nil
		case strings.HasPrefix(n, m.prefix+"/"):
			n = strings.TrimPrefix(n, m.prefix+"/")
		default:
			return "", fmt.Errorf("%w: entry outside the top-level directory", errTemplateUnusable)
		}
	}
	if n == "" {
		return "", nil
	}
	c := path.Clean(n)
	if c == "." {
		return "", nil
	}
	if path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%w: unsafe entry path", errTemplateUnusable)
	}
	return c, nil
}

func pumpTemplateTar(tr *tar.Reader, first *tar.Header, mp *templateNameMapper, w io.Writer, overlayPath string, overlay []byte) error {
	tw := tar.NewWriter(w)
	overlaid := false

	writeOverlay := func() error {
		overlaid = true
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: overlayPath, Mode: 0o644, Size: int64(len(overlay))}); err != nil {
			return err
		}
		_, err := tw.Write(overlay)
		return err
	}

	handle := func(hdr *tar.Header) error {
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			return nil
		case tar.TypeReg, tar.TypeDir:
		default:
			return fmt.Errorf("%w: unsupported entry type", errTemplateUnusable)
		}
		rel, err := mp.rel(hdr.Name)
		if err != nil || rel == "" {
			return err
		}
		if hdr.Typeflag == tar.TypeDir {
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: rel + "/", Mode: 0o755})
		}
		if rel == overlayPath {
			if overlaid {
				return fmt.Errorf("%w: duplicate %s", errTemplateUnusable, overlayPath)
			}
			return writeOverlay()
		}
		mode := int64(0o644)
		if hdr.Mode&0o111 != 0 {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: rel, Mode: mode, Size: hdr.Size}); err != nil {
			return err
		}
		_, err = io.Copy(tw, tr)
		return err
	}

	if err := handle(first); err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errTemplateUnusable) {
				return err
			}
			return fmt.Errorf("%w: %v", errTemplateUnusable, err)
		}
		if err := handle(hdr); err != nil {
			return err
		}
	}
	if !overlaid {
		if err := writeOverlay(); err != nil {
			return err
		}
	}
	return tw.Close()
}

// versionFromTarballPrefix extracts the commit sha GitHub appends to the
// top-level directory ("<owner>-<repo>-<sha>"). "" when the last segment is not
// a 7-40 character hex string.
func versionFromTarballPrefix(prefix string) string {
	i := strings.LastIndexByte(prefix, '-')
	if i < 0 {
		return ""
	}
	sha := prefix[i+1:]
	if len(sha) < 7 || len(sha) > 40 {
		return ""
	}
	for _, c := range sha {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return sha
}

// renderOrbitConfig returns the content of src/orbit.config.ts: public
// connection info only (API base URL and app name), never a secret.
func renderOrbitConfig(apiBaseURL, appName string) []byte {
	var b strings.Builder
	b.WriteString("// GENERATED at builder session Init. Do not hand-edit and do not add secrets.\n")
	b.WriteString("// Holds only public connection info for the Orbit backend app this frontend\n")
	b.WriteString("// talks to.\n")
	b.WriteString("export const ORBIT_API_URL: string = " + tsStringLiteral(apiBaseURL) + "\n")
	b.WriteString("export const ORBIT_APP_NAME: string = " + tsStringLiteral(appName) + "\n")
	return []byte(b.String())
}

// tsStringLiteral quotes s as a single-quoted TypeScript string literal.
func tsStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch {
		case r == '\\' || r == '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
