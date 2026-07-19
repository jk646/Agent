package filetool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ResolvedPath struct {
	Relative string
	Absolute string
}

type Resolver struct {
	root string
}

func NewResolver(root string) (*Resolver, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace is not a directory")
	}
	return &Resolver{root: filepath.Clean(canonical)}, nil
}

func (r *Resolver) Root() string { return r.root }

func (r *Resolver) Resolve(path string, allowMissing bool) (ResolvedPath, error) {
	if filepath.IsAbs(path) {
		return ResolvedPath{}, ErrOutsideWorkspace
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." {
		clean = ""
	}
	target := filepath.Join(r.root, clean)
	if err := ensureWithin(r.root, target); err != nil {
		return ResolvedPath{}, err
	}
	canonical, err := canonicalizeTarget(target, allowMissing)
	if err != nil {
		return ResolvedPath{}, err
	}
	if err := ensureWithin(r.root, canonical); err != nil {
		return ResolvedPath{}, err
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return ResolvedPath{}, ErrSymlink
	} else if err != nil && !os.IsNotExist(err) {
		return ResolvedPath{}, err
	} else if os.IsNotExist(err) && !allowMissing {
		return ResolvedPath{}, ErrNotFound
	}
	relative, err := filepath.Rel(r.root, target)
	if err != nil {
		return ResolvedPath{}, err
	}
	if relative == "." {
		relative = ""
	}
	return ResolvedPath{Relative: filepath.ToSlash(relative), Absolute: target}, nil
}

func canonicalizeTarget(target string, allowMissing bool) (string, error) {
	canonical, err := filepath.EvalSymlinks(target)
	if err == nil {
		return canonical, nil
	}
	if !os.IsNotExist(err) || !allowMissing {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	ancestor := target
	var suffix []string
	for {
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", ErrOutsideWorkspace
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	canonical, err = filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	for index := len(suffix) - 1; index >= 0; index-- {
		canonical = filepath.Join(canonical, suffix[index])
	}
	return canonical, nil
}

func ensureWithin(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrOutsideWorkspace
	}
	return nil
}
