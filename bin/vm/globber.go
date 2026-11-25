//go:build linux && amd64

package main

import (
	"slices"
	"strings"
)

type Globber struct {
	p       *Process
	flags   uint32
	errfunc uint32
	home    string
	found   []string
	aborted bool
}

func (g *Globber) magic(comp string) bool {
	for i := 0; i < len(comp); i++ {
		switch comp[i] {
		case '\\':
			if g.flags&GLOB_NOESCAPE == 0 {
				i++
			}
		case '*', '?', '[':
			return true
		}
	}

	return false
}

func (g *Globber) literal(comp string) string {
	if g.flags&GLOB_NOESCAPE != 0 {
		return comp
	}

	var b strings.Builder

	for i := 0; i < len(comp); i++ {
		if comp[i] == '\\' && i+1 < len(comp) {
			i++
		}

		b.WriteByte(comp[i])
	}

	return b.String()
}

func (g *Globber) failed(dir string, err error) bool {
	if g.errfunc != 0 {
		p := g.p
		path := p.newString(dir)

		if path != 0 {
			f, _ := p.inst.export("__call_glob_error")
			stop := f.Interface().(func(uintptr, uint32, uint32, uint32) uint32)(p.inst.handle(), g.errfunc, path, uint32(errnoOf(err)))

			p.free(path)

			if stop != 0 {
				g.aborted = true
			}
		}
	}

	if g.flags&GLOB_ERR != 0 {
		g.aborted = true
	}

	return !g.aborted
}

func (g *Globber) names(dir string) ([]string, error) {
	path := dir

	if path == "" {
		path = "."
	}

	names, err := g.p.dirNames(path)

	if err != nil && names == nil {
		return nil, err
	}

	return append([]string{".", ".."}, names...), err
}

func (g *Globber) exists(path string) bool {
	var st Stat

	return g.p.statPath(path, false, &st) == nil
}

func (g *Globber) walk(dir string, comps []string) {
	if g.aborted {
		return
	}

	comp, rest := comps[0], comps[1:]

	if comp == "" {
		if len(rest) > 0 {
			g.walk(dir+"/", rest)
		} else if g.exists(dir + ".") {
			g.found = append(g.found, dir)
		}

		return
	}

	if !g.magic(comp) {
		name := dir + g.literal(comp)

		if len(rest) == 0 {
			if g.exists(name) {
				g.found = append(g.found, name)
			}

			return
		}

		g.walk(name+"/", rest)

		return
	}

	names, err := g.names(dir)

	if err != nil && err != ENOENT && err != ENOTDIR && !g.failed(strings.TrimSuffix(dir, "/"), err) {
		return
	}

	slices.Sort(names)

	flags := 0

	if g.flags&GLOB_PERIOD == 0 {
		flags |= FNM_PERIOD
	}

	if g.flags&GLOB_NOESCAPE != 0 {
		flags |= FNM_NOESCAPE
	}

	for _, name := range names {
		if (name == "." || name == "..") && !strings.HasPrefix(comp, ".") {
			continue
		}

		if !fnmatch([]byte(comp), []byte(name), flags, true) {
			continue
		}

		if len(rest) == 0 {
			g.found = append(g.found, dir+name)
		} else {
			g.walk(dir+name+"/", rest)
		}
	}
}

func (g *Globber) tilde(pattern string) (string, bool) {
	if g.flags&(GLOB_TILDE|GLOB_TILDE_CHECK) == 0 || !strings.HasPrefix(pattern, "~") {
		return pattern, true
	}

	_, rest, slash := strings.Cut(pattern[1:], "/")
	home := g.home

	if home == "" {
		return pattern, g.flags&GLOB_TILDE_CHECK == 0
	}

	if !slash {
		return home, true
	}

	return strings.TrimSuffix(home, "/") + "/" + rest, true
}
