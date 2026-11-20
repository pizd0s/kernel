package main

type Background int

func (b Background) Error() string {
	return b.error()
}

func (b Background) error() string {
	return "out of the foreground: " + signalName(int(b))
}
