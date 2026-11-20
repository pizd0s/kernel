package main

import (
	"strconv"
	"strings"
)

var typeLetters = map[ValType]byte{I32: 'i', I64: 'I', F32: 'f', F64: 'F', V128: 'v', FuncRef: 'r', ExternRef: 'e'}

const (
	symInit          = "w2o.init"
	symMorestackStub = "w2o.morestack"
	symImport        = "w2o.import."
	symExport        = "w2o.export."
	symTrap          = "w2o.trap"
	symTrapAt        = "w2o.trap_at"
	symAsanRange     = "w2o.asan_range"
	symMemoryGrow    = "w2o.memory_grow"
	symTableGrow     = "w2o.table_grow"
	symInterruptCall = "w2o.interrupt_call"
	symStackGrow     = "w2o.stack_grow"
	symStr           = "str."
)

func parseTypes(s string) ([]ValType, bool) {
	ts := make([]ValType, len(s))

	for i := range s {
		found := false

		for t, c := range typeLetters {
			if c == s[i] {
				ts[i], found = t, true
			}
		}

		if !found {
			return nil, false
		}
	}

	return ts, true
}

func parseSymbolPart(s string) (string, bool) {
	b := make([]byte, 0, len(s))

	for i := 0; i < len(s); i++ {
		if s[i] != '_' {
			b = append(b, s[i])

			continue
		}

		if i+3 > len(s) {
			return "", false
		}

		x, err := strconv.ParseUint(s[i+1:i+3], 16, 8)

		if err != nil {
			return "", false
		}

		b = append(b, byte(x))
		i += 2
	}

	return string(b), true
}

func parseFuncSymbol(sym, prefix string, names int) ([]string, *FuncType, bool) {
	if !strings.HasPrefix(sym, prefix) {
		return nil, nil, false
	}

	parts := strings.Split(sym[len(prefix):], ".")

	if len(parts) != names+2 {
		return nil, nil, false
	}

	out := make([]string, names)

	for i := range out {
		name, ok := parseSymbolPart(parts[i])

		if !ok {
			return nil, nil, false
		}

		out[i] = name
	}

	params, ok1 := parseTypes(parts[names])
	results, ok2 := parseTypes(parts[names+1])

	return out, &FuncType{params: params, results: results}, ok1 && ok2
}
