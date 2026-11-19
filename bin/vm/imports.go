//go:build linux && amd64

package main

type Imports interface {
	function(imp *ImportMeta) any

	global(imp *ImportMeta) uintptr

	table(imp *ImportMeta) uintptr
}

type FunctionImports struct{}

func (FunctionImports) global(*ImportMeta) uintptr {
	return 0
}

func (FunctionImports) table(*ImportMeta) uintptr {
	return 0
}
