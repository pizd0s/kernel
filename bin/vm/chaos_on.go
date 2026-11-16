//go:build pizdoschaos

package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// Rules are deterministic one-shot failures after N successful visits.
// State lives in the host, and a rule is spent before a resource is acquired.
type ChaosRule struct {
	name string
	skip uint32
}

var chaosState struct {
	mu    sync.Mutex
	rules map[string]uint32
}

func chaosRules(text string, points map[string]uint32) []ChaosRule {
	var rules []ChaosRule
	seen := map[string]bool{}

	if text == "" {
		return rules
	}

	for _, field := range strings.Split(text, ",") {
		name, count, ok := strings.Cut(field, "@")
		n, err := strconv.ParseUint(count, 10, 32)
		_, known := points[name]

		if !ok || err != nil || !known || seen[name] {
			throwFmt("invalid chaos rule %q", field)
		}

		seen[name] = true
		rules = append(rules, ChaosRule{name, uint32(n)})
	}

	return rules
}

// vm's own points, of its compiler and its loader, are PIZDOS_VM_CHAOS's,
// of vm's environment, for the whole run. The kernel's points are a tree
// of processes': a process whose program starts with PIZDOS_CHAOS in its
// environment, and has no rules yet, makes them of it (chaosStart), and
// its children of fork and the programs they exec share them: the visits
// of all of them count, and nothing else's.
var (
	vmPoints      = map[string]uint32{"mmap": 0, "compile-read": 0, "compile-write": 0}
	processPoints = map[string]uint32{"openat": 0, "read": 0, "write": 0, "fork": 0, "file-stat": 0, "readlink": 0, "memory-grow": 0}
)

func setupVmChaos() {
	chaosState.rules = map[string]uint32{}

	for _, rule := range chaosRules(os.Getenv("PIZDOS_VM_CHAOS"), vmPoints) {
		chaosState.rules[rule.name] = rule.skip
	}
}

// chaosFail is a visit of one of vm's points.
func chaosFail(point string) bool {
	chaosState.mu.Lock()
	defer chaosState.mu.Unlock()

	n, ok := chaosState.rules[point]

	if !ok {
		return false
	}

	if n > 0 {
		chaosState.rules[point] = n - 1

		return false
	}

	delete(chaosState.rules, point)

	return true
}

// chaosRulesOf is the rules of a tree of processes, which mu guards.
type chaosRulesOf struct {
	mu    sync.Mutex
	rules map[string]uint32
}

// chaosStart makes the process's rules of the environment its program
// starts with, unless it has some: an invalid one ends it.
func (p *Process) chaosStart(env []string) {
	if p.chaos != nil {
		return
	}

	for _, e := range env {
		if text, ok := strings.CutPrefix(e, "PIZDOS_CHAOS="); ok {
			c := &chaosRulesOf{rules: map[string]uint32{}}

			for _, rule := range chaosRules(text, processPoints) {
				c.rules[rule.name] = rule.skip
			}

			p.chaos = c
		}
	}
}

// chaosFail is a visit of a point of the process's: the one its rule
// fails, after the visits it skips.
func (p *Process) chaosFail(point string) bool {
	if p == nil || p.chaos == nil {
		return false
	}

	c := p.chaos
	c.mu.Lock()
	defer c.mu.Unlock()

	n, ok := c.rules[point]

	if !ok {
		return false
	}

	if n > 0 {
		c.rules[point] = n - 1

		return false
	}

	delete(c.rules, point)

	return true
}

func setupCompilerChaos(i *Instance) {
	points := map[string]uint32{"read": 0, "write": 1, "grow": 2}
	rules := chaosRules(os.Getenv("PIZDOS_AOT_CHAOS"), points)

	for _, rule := range rules {
		entry, _ := i.export("chaosConfigure")
		entry.Interface().(func(uintptr, uint32, uint32))(i.ctx(), points[rule.name], rule.skip)
	}
}
