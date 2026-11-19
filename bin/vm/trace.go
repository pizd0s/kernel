//go:build linux && amd64

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var loaded struct {
	sync.Mutex
	ranges []CodeRange
}

const (
	traceDepth   = 1024
	tracePrinted = 64
)

type CodeRange struct {
	base, end uintptr
	name      string
}

func noteCode(name string, base, end uintptr) {
	loaded.Lock()
	loaded.ranges = append(loaded.ranges, CodeRange{base, end, name})
	loaded.Unlock()
}

func codeAt(pc uintptr) (CodeRange, bool) {
	loaded.Lock()

	defer loaded.Unlock()

	for _, r := range loaded.ranges {
		if pc >= r.base && pc < r.end {
			return r, true
		}
	}

	return CodeRange{}, false
}

func callers(skip int) []uintptr {
	pcs := make([]uintptr, traceDepth)

	return pcs[:runtime.Callers(skip+2, pcs)]
}

func traceLines(pcs []uintptr) []string {
	type frame struct {
		name, place string
		module      bool
	}

	var frames []frame

	it := runtime.CallersFrames(pcs)

	for {
		f, more := it.Next()

		if f.PC != 0 {
			if code, ok := codeAt(f.PC); ok {
				if !strings.HasPrefix(f.Function, "w2o.") {
					fr := frame{name: fmt.Sprintf("%s+%#x", moduleFunction(f.Function), f.PC-f.Entry), module: true}

					if f.File != code.name {
						fr.place = fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)
					}

					frames = append(frames, fr)
				}
			} else {
				frames = append(frames, frame{name: goFunction(f.Function), place: fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)})
			}
		}

		if !more {
			break
		}
	}

	last := -1

	for i, f := range frames {
		if f.module {
			last = i
		}
	}

	if last >= 0 {
		frames = frames[:last+1]
	}

	width := 0

	for _, f := range frames {
		if f.place != "" && len(f.name) > width && len(f.name) <= 48 {
			width = len(f.name)
		}
	}

	var lines []string

	emit := func(f frame) {
		if f.place == "" {
			lines = append(lines, f.name)
		} else {
			lines = append(lines, fmt.Sprintf("%-*s  %s", width, f.name, f.place))
		}
	}

	if len(frames) <= tracePrinted {
		for _, f := range frames {
			emit(f)
		}

		return lines
	}

	head, tail := tracePrinted/2, tracePrinted/2-1

	for _, f := range frames[:head] {
		emit(f)
	}

	lines = append(lines, fmt.Sprintf("... %d frames", len(frames)-head-tail))

	for _, f := range frames[len(frames)-tail:] {
		emit(f)
	}

	return lines
}

func goFunction(name string) string {
	start := strings.LastIndexByte(name, '/') + 1
	dot := strings.IndexByte(name[start:], '.') + 1

	return name[start+dot:]
}

func moduleFunction(registered string) string {
	if _, name, ok := strings.Cut(registered, ":"); ok {
		return name
	}

	return registered
}
