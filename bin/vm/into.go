package main

type Into struct {
	p      *Process
	at, n  uint32
	vector bool
	to     *MemfdFile
}

func (d Into) put(at int, data []byte) int {
	switch {
	case d.to != nil:
		n, _ := d.to.with(int64(at), len(data), true, func(b []byte) (int, error) {
			return copy(b, data), nil
		})

		return n
	case d.vector:
		var few [4][]byte

		spans, _ := d.p.ioSpansIn(few[:0], d.at, d.n)

		return scatterInto(advance(spans, at), data)
	}

	return copy(d.p.span(d.at+uint32(at), d.n-uint32(at)), data)
}
