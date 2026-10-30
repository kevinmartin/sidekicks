package guest

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"syscall"
)

func workspacePath(raw string, allowRoot bool) (string, error) {
	if raw == "" && allowRoot {
		return ".", nil
	}
	if len(raw) > 1024 || strings.ContainsAny(raw, "\x00\\") || strings.HasPrefix(raw, "/") {
		return "", errors.New("invalid workspace path")
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != raw {
		return "", errors.New("a normalized relative workspace path is required")
	}
	for part := range strings.SplitSeq(raw, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid path component")
		}
	}
	return clean, nil
}

// Root makes filesystem traversal confinement resistant to symlink/rename races.
// Additionally reject observed symlinks and final-component links, rather than
// providing a symlink-following file API. The workspace must not contain mounts
// or device nodes; the runtime supplies it as a private unprivileged tmpfs.
func (s *Server) checkedPath(raw string, allowRoot, allowMissingFinal bool) (string, error) {
	clean, err := workspacePath(raw, allowRoot)
	if err != nil {
		return "", err
	}
	if clean == "." {
		return clean, nil
	}
	parts := strings.Split(clean, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		st, err := s.root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) && allowMissingFinal && i == len(parts)-1 {
			return clean, nil
		}
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlinks are not supported")
		}
		if i < len(parts)-1 && !st.IsDir() {
			return "", errors.New("parent is not a directory")
		}
	}
	return clean, nil
}
func (s *Server) openDirectory(raw string) (*os.File, error) {
	clean, err := s.checkedPath(raw, true, false)
	if err != nil {
		return nil, err
	}
	return s.root.OpenFile(clean, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
}
func (s *Server) filePath(r *http.Request) (string, error) {
	if len(r.URL.Query()) != 1 || len(r.URL.Query()["path"]) != 1 {
		return "", errors.New("one path query is required")
	}
	return s.checkedPath(r.URL.Query().Get("path"), false, r.Method == http.MethodPut)
}
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	name, err := s.filePath(r)
	if err != nil {
		fileError(w, err)
		return
	}
	f, err := s.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		fileError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "only regular workspace files are supported")
		return
	}
	if st.Size() > MaxFileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds 1 mebibytes")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "file read failed")
		return
	}
	if len(data) > MaxFileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds 1 mebibytes")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request) {
	name, err := s.filePath(r)
	if err != nil {
		fileError(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFileBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds 1 mebibytes")
		return
	}
	dir := path.Dir(name)
	parentRoot, err := s.root.OpenRoot(dir)
	if err != nil {
		fileError(w, err)
		return
	}
	defer parentRoot.Close()
	if err = writeDurable(parentRoot, path.Base(name), body); err != nil {
		writeError(w, http.StatusInternalServerError, "durable file write failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bytes": len(body)})
}

// writeDurable anchors every operation to the same opened parent. A guest
// renaming that directory cannot redirect temp creation or make us fsync a
// different directory from the one receiving the atomic rename.
func writeDurable(parentRoot *os.Root, name string, body []byte) error {
	parent, err := parentRoot.OpenFile(".", os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer parent.Close()
	validateTarget := func() error {
		st, err := parentRoot.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return errors.New("target is not a regular file")
		}
		return nil
	}
	if err = validateTarget(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".sidekicks-write-" + hex.EncodeToString(nonce[:])
	f, err := parentRoot.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer parentRoot.Remove(temp)
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = validateTarget()
	}
	if err == nil {
		err = parentRoot.Rename(temp, name)
	}
	if err == nil {
		err = parent.Sync()
	}
	return err
}

func fileError(w http.ResponseWriter, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "workspace file not found")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid workspace file path")
}
