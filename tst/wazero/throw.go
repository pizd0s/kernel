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

func throwFmt(format string, args ...any) {
	throw(fmt.Errorf(format, args...))
}

func try(cb func()) (caught *Exception) {
	defer func() {
		if e := recover(); e != nil {
			exception, ok := e.(*Exception)

			if !ok {
				panic(e)
			}

			caught = exception
		}
	}()

	cb()

	return nil
}
