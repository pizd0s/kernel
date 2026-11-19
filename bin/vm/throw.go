package main

import "fmt"

type Exception struct{ cause error }

func (e *Exception) Error() string {
	return e.error()
}

func (e *Exception) error() string {
	return e.cause.Error()
}

func throw(err error) {
	if err != nil {
		panic(&Exception{err})
	}
}

func throw2[T any](value T, err error) T {
	throw(err)

	return value
}

func throw3[T, U any](value T, other U, err error) (T, U) {
	throw(err)

	return value, other
}

func throwFmt(format string, args ...any) {
	throw(fmt.Errorf(format, args...))
}

func try(cb func()) *Exception {
	return tryAs[*Exception](cb)
}

func tryAs[T any](cb func()) (caught T) {
	defer func() {
		if e := recover(); e != nil {
			value, ok := e.(T)

			if !ok {
				panic(e)
			}

			caught = value
		}
	}()

	cb()

	return
}
