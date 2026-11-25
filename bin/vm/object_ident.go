package main

import "sync/atomic"

var objectIDs atomic.Uint64

type ObjectIdent struct {
	id uint64
}

func newObjectIdent() ObjectIdent {
	return ObjectIdent{objectIDs.Add(1)}
}

func (o *ObjectIdent) objectID() uint64 {
	return o.id
}
