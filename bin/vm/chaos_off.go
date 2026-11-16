//go:build !pizdoschaos

package main

// The compiler inlines these away in production builds, as in monorepo/ay.
func chaosFail(string) bool {
	return false
}

func setupVmChaos() {}

// chaosRulesOf is the rules of a tree of processes, none in production.
type chaosRulesOf struct{}

func (p *Process) chaosStart([]string) {}

func (p *Process) chaosFail(string) bool {
	return false
}

func setupCompilerChaos(*Instance) {}
