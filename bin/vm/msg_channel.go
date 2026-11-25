package main

type MsgChannel[T any] struct {
	buf   []T
	head  int
	count int
}

func (c *MsgChannel[T]) put(v T) {
	if c.count == len(c.buf) {
		c.grow()
	}

	c.buf[(c.head+c.count)&(len(c.buf)-1)] = v
	c.count++
}

func (c *MsgChannel[T]) grow() {
	buf := make([]T, max(2*len(c.buf), 1))
	n := copy(buf, c.buf[c.head:])

	copy(buf[n:], c.buf[:c.head])
	c.buf, c.head = buf, 0
}

func (c *MsgChannel[T]) take() (v T, ok bool) {
	if c.count == 0 {
		return v, false
	}

	var none T

	v, c.buf[c.head] = c.buf[c.head], none
	c.head = (c.head + 1) & (len(c.buf) - 1)
	c.count--

	return v, true
}

func (c *MsgChannel[T]) peek() (v T, ok bool) {
	if c.count == 0 {
		return v, false
	}

	return c.buf[c.head], true
}

func (c *MsgChannel[T]) len() int {
	return c.count
}
