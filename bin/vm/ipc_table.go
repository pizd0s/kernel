package main

const ipcMax = 32000

type IpcTable[T any] struct {
	keys    map[int32]int32
	objects map[int32]T
	next    int32
}

func newIpcTable[T any]() IpcTable[T] {
	return IpcTable[T]{keys: map[int32]int32{}, objects: map[int32]T{}}
}

func (t *IpcTable[T]) add(key int32, o T) int32 {
	id := t.next

	t.next++
	t.objects[id] = o

	if key != IPC_PRIVATE {
		t.keys[key] = id
	}

	return id
}

func (t *IpcTable[T]) remove(id, key int32) {
	delete(t.objects, id)

	if key != IPC_PRIVATE && t.keys[key] == id {
		delete(t.keys, key)
	}
}
