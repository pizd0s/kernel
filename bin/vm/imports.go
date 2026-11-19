//go:build linux && amd64

package main

// Imports resolves what an instance imports.
type Imports interface {
	// function returns a Go function of the import's type,
	// func(uintptr, params...) (results...), no closure, the first argument
	// the instance; or the address of code of that convention, a stub of
	// bindContext, say.
	function(imp *ImportMeta) any
	// global returns the address of an imported global's value.
	global(imp *ImportMeta) uintptr
	// table returns the address of a table's {entries, size, max}.
	table(imp *ImportMeta) uintptr
}

// FunctionImports supplies the empty state imports of function-only hosts.
type FunctionImports struct{}

func (FunctionImports) global(*ImportMeta) uintptr {
	return 0
}

func (FunctionImports) table(*ImportMeta) uintptr {
	return 0
}
