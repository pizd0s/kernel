//go:build pizdoschaos

package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

var vmPoints = map[string]uint32{"mmap": 0}

var processPoints = map[string]uint32{"ctl": 0, "read": 0, "write": 0, "fork": 0, "file-stat": 0, "memory-grow": 0, "memory-move": 0,
	"memory-compact": 0}

var chaosState struct {
	mu    sync.Mutex
	rules map[string]uint32
}

type ChaosRule struct {
	name string
	skip uint32
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

func setupVmChaos() {
	chaosState.rules = map[string]uint32{}

	for _, rule := range chaosRules(os.Getenv("PIZDOS_VM_CHAOS"), vmPoints) {
		chaosState.rules[rule.name] = rule.skip
	}
}

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

type ChaosRulesOf struct {
	mu    sync.Mutex
	rules map[string]uint32
}

func chaosRulesIn(env []string) *ChaosRulesOf {
	var c *ChaosRulesOf

	for _, e := range env {
		if text, ok := strings.CutPrefix(e, "PIZDOS_CHAOS="); ok {
			c = &ChaosRulesOf{rules: map[string]uint32{}}

			for _, rule := range chaosRules(text, processPoints) {
				c.rules[rule.name] = rule.skip
			}
		}
	}

	return c
}

func (c *ChaosRulesOf) fail(point string) bool {
	if c == nil {
		return false
	}

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

func (c *ChaosRulesOf) holds(point string) bool {
	if c == nil {
		return false
	}

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

	return true
}

func compilerEnv() []string {
	if rules, ok := os.LookupEnv("PIZDOS_AOT_CHAOS"); ok {
		return []string{"PIZDOS_AOT_CHAOS=" + rules}
	}

	return nil
}
