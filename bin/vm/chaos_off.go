//go:build !pizdoschaos

package main

func chaosFail(string) bool {
	return false
}

func setupVmChaos() {
}

type ChaosRulesOf struct{}

func chaosRulesIn([]string) *ChaosRulesOf {
	return nil
}

func (*ChaosRulesOf) fail(string) bool {
	return false
}

func (*ChaosRulesOf) holds(string) bool {
	return false
}

func setupCompilerChaos(*Instance) {
}
