package bcl

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Lockfile struct {
	Version string      `json:"version"`
	Modules []LockEntry `json:"modules"`
}

type LockEntry struct {
	Source     string `json:"source"`
	Resolved   string `json:"resolved,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Version    string `json:"version,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Kind       string `json:"kind"`
	Provenance string `json:"provenance,omitempty"`
	ArchiveURL string `json:"archive_url,omitempty"`
	Format     string `json:"format,omitempty"`
	Extracted  string `json:"extracted,omitempty"`
}

type ModuleFetchOptions struct {
	CacheDir string
	Client   *http.Client
}

type ModuleVerifyOptions struct {
	CacheDir string
}

func GenerateLockfile(doc *Document, baseDir string) (*Lockfile, error) {
	lock := &Lockfile{Version: "1"}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *ImportDecl:
				lock.Modules = append(lock.Modules, lockEntriesForSource(x.Path, baseDir)...)
			case *Block:
				if x.Type == "module" {
					for _, item := range x.Body {
						if a, ok := item.(*Assignment); ok && a.Name == "source" {
							if lit, ok := a.Value.(*Literal); ok {
								lock.Modules = append(lock.Modules, lockEntriesForSource(lit.Data.(string), baseDir)...)
							}
						}
					}
				}
				walk(x.Body)
			}
		}
	}
	walk(doc.Items)
	return lock, nil
}

func WriteLockfile(path string, lock *Lockfile) error {
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0644)
}

func ReadLockfile(path string) (*Lockfile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock Lockfile
	if err := json.Unmarshal(b, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func (l *Lockfile) Find(source string) *LockEntry {
	if l == nil {
		return nil
	}
	for i := range l.Modules {
		if l.Modules[i].Source == source || l.Modules[i].Resolved == source {
			return &l.Modules[i]
		}
	}
	return nil
}

func VerifyLockEntry(entry LockEntry) error {
	if entry.Checksum == "" || entry.Resolved == "" {
		return nil
	}
	if entry.Kind != "local" && entry.Extracted != "" {
		if _, err := os.Stat(entry.Extracted); err != nil {
			return err
		}
		return nil
	}
	b, err := os.ReadFile(entry.Resolved)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	got := "sha256:" + hex.EncodeToString(sum[:])
	if got != entry.Checksum {
		return fmt.Errorf("lock checksum mismatch for %s: got %s want %s", entry.Resolved, got, entry.Checksum)
	}
	return nil
}

func lockEntry(source, baseDir string) LockEntry {
	e := LockEntry{Source: source}
	switch {
	case strings.HasPrefix(source, "git::") || strings.HasSuffix(source, ".git"):
		e.Kind = "git"
		e.Resolved = source
		if i := strings.Index(source, "?ref="); i >= 0 {
			e.Resolved = source[:i]
			e.Revision = source[i+5:]
		}
	case strings.Contains(source, "://"):
		e.Kind = "registry"
		e.Resolved = source
		e.Provenance = "remote"
	default:
		e.Kind = "local"
		if !filepath.IsAbs(source) {
			e.Resolved = filepath.Clean(filepath.Join(baseDir, source))
		} else {
			e.Resolved = source
		}
		if b, err := os.ReadFile(e.Resolved); err == nil {
			sum := sha256.Sum256(b)
			e.Checksum = "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	return e
}

func FetchModules(path string, lockfile string, opts *ModuleFetchOptions) error {
	doc, err := ParsePath(path)
	if err != nil {
		return err
	}
	lock, err := GenerateLockfile(doc, filepath.Dir(path))
	if err != nil {
		return err
	}
	if lockfile != "" {
		if existing, err := ReadLockfile(lockfile); err == nil {
			lock = existing
		}
	}
	cacheDir := moduleCacheDir("")
	client := http.DefaultClient
	if opts != nil {
		if opts.CacheDir != "" {
			cacheDir = opts.CacheDir
		}
		if opts.Client != nil {
			client = opts.Client
		}
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	for i := range lock.Modules {
		entry := &lock.Modules[i]
		if entry.Kind == "local" {
			continue
		}
		if strings.HasPrefix(entry.Source, "git::") || strings.HasSuffix(entry.Source, ".git") {
			return fmt.Errorf("git module fetch is not implemented for %q; use registry archive URLs", entry.Source)
		}
		if err := fetchRegistryArchive(entry, cacheDir, client); err != nil {
			return err
		}
	}
	if lockfile != "" {
		return WriteLockfile(lockfile, lock)
	}
	return nil
}

func VerifyModules(lockfile string, opts *ModuleVerifyOptions) []Diagnostic {
	lock, err := ReadLockfile(lockfile)
	if err != nil {
		return []Diagnostic{{Severity: "error", Message: err.Error()}}
	}
	cacheDir := moduleCacheDir("")
	if opts != nil && opts.CacheDir != "" {
		cacheDir = opts.CacheDir
	}
	var diags []Diagnostic
	for _, entry := range lock.Modules {
		if entry.Kind == "local" {
			if err := VerifyLockEntry(entry); err != nil {
				diags = append(diags, Diagnostic{Severity: "error", Message: err.Error()})
			}
			continue
		}
		extracted := entry.Extracted
		if extracted == "" {
			extracted = filepath.Join(cacheDir, cacheKey(entry.Source))
		}
		if _, err := os.Stat(extracted); err != nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("module %q not fetched: %v", entry.Source, err)})
		}
	}
	return diags
}

func fetchRegistryArchive(entry *LockEntry, cacheDir string, client *http.Client) error {
	archiveURL := entry.ArchiveURL
	if archiveURL == "" {
		archiveURL = entry.Resolved
	}
	data, err := readArchive(archiveURL, client)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	got := "sha256:" + hex.EncodeToString(sum[:])
	if entry.Checksum != "" && entry.Checksum != got {
		return fmt.Errorf("module checksum mismatch for %s: got %s want %s", entry.Source, got, entry.Checksum)
	}
	entry.Checksum = got
	format := archiveFormat(archiveURL, entry.Format)
	if format == "" {
		return fmt.Errorf("unsupported module archive format for %q", archiveURL)
	}
	dst := filepath.Join(cacheDir, cacheKey(entry.Source))
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	if err := extractArchive(data, format, dst); err != nil {
		return err
	}
	entry.ArchiveURL = archiveURL
	entry.Format = format
	entry.Extracted = dst
	return nil
}

func readArchive(source string, client *http.Client) ([]byte, error) {
	u, err := url.Parse(source)
	if err == nil && u.Scheme == "file" {
		return os.ReadFile(u.Path)
	}
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		resp, err := client.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch %s: %s", source, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(source)
}

func archiveFormat(source, explicit string) string {
	switch explicit {
	case "zip", "tar.gz", "tgz":
		if explicit == "tgz" {
			return "tar.gz"
		}
		return explicit
	}
	if strings.HasSuffix(source, ".zip") {
		return "zip"
	}
	if strings.HasSuffix(source, ".tar.gz") || strings.HasSuffix(source, ".tgz") {
		return "tar.gz"
	}
	return ""
}

func extractArchive(data []byte, format, dst string) error {
	switch format {
	case "zip":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			if err := extractZipFile(f, dst); err != nil {
				return err
			}
		}
		return nil
	case "tar.gz":
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer gr.Close()
		tr := tar.NewReader(gr)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := extractTarFile(h, tr, dst); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func extractZipFile(f *zip.File, dst string) error {
	target, err := safeExtractPath(dst, f.Name)
	if err != nil {
		return err
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func extractTarFile(h *tar.Header, r io.Reader, dst string) error {
	target, err := safeExtractPath(dst, h.Name)
	if err != nil {
		return err
	}
	switch h.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0755)
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode))
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, r)
		return err
	default:
		return nil
	}
}

func safeExtractPath(root, name string) (string, error) {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(root, clean)
	if !strings.HasPrefix(target, filepath.Clean(root)+string(filepath.Separator)) && filepath.Clean(target) != filepath.Clean(root) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return target, nil
}

func moduleCacheDir(base string) string {
	if base != "" {
		return base
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "bcl", "modules")
	}
	return filepath.Join(os.TempDir(), "bcl-modules")
}

func cacheKey(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func lockEntriesForSource(source, baseDir string) []LockEntry {
	if isRemoteSource(source) {
		return []LockEntry{lockEntry(source, baseDir)}
	}
	files, err := resolveModuleOrSource(source, baseDir)
	if err != nil {
		return []LockEntry{lockEntry(source, baseDir)}
	}
	out := make([]LockEntry, 0, len(files))
	for _, f := range files {
		out = append(out, lockEntry(f, ""))
	}
	return out
}

func resolveModuleOrSource(source, baseDir string) ([]string, error) {
	if strings.ContainsAny(source, "*?[") {
		return resolveSourceFiles(source, baseDir)
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(baseDir, source)
	}
	st, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return resolveModuleFiles(source, "")
	}
	return []string{source}, nil
}

type WatchEvent struct {
	Path        string
	Normalized  *Normalized
	Diagnostics []Diagnostic
	Error       error
	Dependency  string
}

type Watcher struct {
	Path     string
	Options  *Options
	Interval time.Duration
}

func Watch(path string, opts *Options, onChange func(WatchEvent)) chan struct{} {
	w := &Watcher{Path: path, Options: opts, Interval: time.Second}
	return w.Start(onChange)
}

func (w *Watcher) Start(onChange func(WatchEvent)) chan struct{} {
	stop := make(chan struct{})
	if w.Interval <= 0 {
		w.Interval = time.Second
	}
	go func() {
		var last time.Time
		tick := time.NewTicker(w.Interval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				st, err := os.Stat(w.Path)
				if err != nil {
					onChange(WatchEvent{Path: w.Path, Error: err})
					continue
				}
				if st.ModTime().Equal(last) {
					continue
				}
				last = st.ModTime()
				n, err := CompileFile(w.Path, w.Options)
				ev := WatchEvent{Path: w.Path, Normalized: n, Error: err}
				if err != nil {
					if e, ok := err.(ErrorList); ok {
						ev.Diagnostics = e
					}
				}
				onChange(ev)
			}
		}
	}()
	return stop
}
