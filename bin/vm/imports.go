//go:build linux && amd64

package main

type Imports interface {
	function(imp *ImportMeta) any
}
