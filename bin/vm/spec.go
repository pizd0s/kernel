package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

var specTraps = []struct {
	prefix string
	code   uint32
}{
	{"unreachable", trapUnreachable}, {"out of bounds memory access", trapMemoryOOB},
	{"integer overflow", trapIntOverflow}, {"integer divide by zero", trapDivByZero},
	{"invalid conversion to integer", trapConversion}, {"indirect call type mismatch", trapIndirectType},
	{"undefined element", trapUndefinedElem}, {"uninitialized element", trapUninitElem},
	{"call stack exhausted", trapStack}, {"out of bounds table access", trapTableOOB},
}

var spectestFunctions = map[string]any{
	"print":         func() {},
	"print_i32":     func(x uint32) {},
	"print_i64":     func(x uint64) {},
	"print_f32":     func(x float32) {},
	"print_f64":     func(x float64) {},
	"print_i32_f32": func(x uint32, y float32) {},
	"print_f64_f64": func(x, y float64) {},
}

const specStack = 8 << 20

type SpecValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type SpecAction struct {
	Type   string      `json:"type"`
	Module string      `json:"module"`
	Field  string      `json:"field"`
	Args   []SpecValue `json:"args"`
}

type SpecCmd struct {
	Type       string      `json:"type"`
	Line       int         `json:"line"`
	Filename   string      `json:"filename"`
	Name       string      `json:"name"`
	Action     *SpecAction `json:"action"`
	Expected   []SpecValue `json:"expected"`
	Text       string      `json:"text"`
	ModuleType string      `json:"module_type"`
}

type SpecRunner struct {
	k       *Kernel
	dir     string
	current *Instance
	named   map[string]*Instance

	live                    map[*Instance]bool
	passed, failed, skipped int
	unsupported             map[string]bool
}

func specCommand(args []string) int {
	if len(args) != 1 {
		usage()
	}

	var file struct {
		Commands []SpecCmd `json:"commands"`
	}

	throw(json.Unmarshal(throw2(os.ReadFile(args[0])), &file))

	r := &SpecRunner{k: newKernel(Cred{}, defaultMemory), dir: filepath.Dir(args[0]), named: map[string]*Instance{}, live: map[*Instance]bool{},
		unsupported: map[string]bool{}}

	for i := range file.Commands {
		r.command(&file.Commands[i])
	}

	fmt.Printf("RESULT %d %d %d\n", r.passed, r.failed, r.skipped)
	var notes []string

	for n := range r.unsupported {
		notes = append(notes, n)
	}

	sort.Strings(notes)

	for _, n := range notes {
		fmt.Printf("UNSUPPORTED %s\n", n)
	}

	runtime.KeepAlive(r)

	return 0
}

func (r *SpecRunner) fail(line int, format string, args ...any) {
	r.failed++
	fmt.Printf("FAIL line %d: %s\n", line, fmt.Sprintf(format, args...))
}

func findExport(i *Instance, name string, kind byte) *ExportMeta {
	for k := range i.obj.meta.exports {
		if e := &i.obj.meta.exports[k]; e.name == name && e.kind == kind {
			return e
		}
	}

	return nil
}

func (r *SpecRunner) function(imp *ImportMeta) any {
	if imp.module == "spectest" {
		return spectestFunctions[imp.name]
	}

	return nil
}

func globalAddr(i *Instance, index uint32) uintptr {
	return i.ctx() + uintptr(i.obj.meta.globals[index].offset)
}

func (r *SpecRunner) load(filename string) *Instance {
	var i *Instance

	if e := try(func() {
		data := throw2(os.ReadFile(filepath.Join(r.dir, filename)))
		obj := readObject(throw2(r.k.compileObject(data, CompileOptions{})))

		i = instantiate(r.k.pool, filename, obj, r, specStack)
	}); e != nil {
		r.unsupported[e.error()] = true

		return nil
	}

	return i
}

func (r *SpecRunner) command(c *SpecCmd) {
	switch c.Type {
	case "module":
		r.current = nil

		if c.ModuleType == "text" {
			r.skipped++

			return
		}

		i := r.load(c.Filename)

		if c.Name != "" {
			r.named[c.Name] = i
		}

		if i == nil {
			r.skipped++

			return
		}

		r.current = i

		if code := trapOf(i.start); code >= 0 {
			r.fail(c.Line, "instantiation trapped: %d", code)

			return
		}

		r.live[i] = true
		r.passed++
	case "assert_return", "action":
		r.assertReturn(c)
	case "assert_trap", "assert_exhaustion":
		r.assertTrap(c)
	case "assert_uninstantiable":
		i := r.load(c.Filename)

		if i == nil {
			r.skipped++

			return
		}

		if trapOf(i.start) < 0 {
			r.fail(c.Line, "instantiation did not trap")

			return
		}

		r.passed++
	default:
		r.skipped++
	}
}

func (r *SpecRunner) moduleOf(a *SpecAction) *Instance {
	i := r.current

	if a.Module != "" {
		i = r.named[a.Module]
	}

	if i == nil || !r.live[i] {
		return nil
	}

	return i
}

func specArg(i *Instance, v SpecValue) (reflect.Value, bool) {
	var s string

	if json.Unmarshal(v.Value, &s) != nil {
		return reflect.Value{}, false
	}

	if v.Type == "funcref" || v.Type == "externref" {
		if s == "null" {
			return reflect.ValueOf(uintptr(0)), true
		}

		x, err := strconv.ParseUint(s, 10, 64)

		if err != nil {
			return reflect.Value{}, false
		}

		if v.Type == "funcref" {
			ref, ok := i.specFunctionRef(x)

			return reflect.ValueOf(ref), ok
		}

		return reflect.ValueOf(uintptr(x + 1)), true
	}

	x, err := strconv.ParseUint(s, 10, 64)

	if err != nil {
		return reflect.Value{}, false
	}

	switch v.Type {
	case "i32":
		return reflect.ValueOf(uint32(x)), true
	case "i64":
		return reflect.ValueOf(x), true
	case "f32":
		return reflect.ValueOf(math.Float32frombits(uint32(x))), true
	case "f64":
		return reflect.ValueOf(math.Float64frombits(x)), true
	}

	return reflect.Value{}, false
}

func (r *SpecRunner) invoke(a *SpecAction) (func() []reflect.Value, bool) {
	if a == nil {
		return nil, false
	}

	i := r.moduleOf(a)

	if i == nil {
		return nil, false
	}

	if a.Type == "get" {
		e := findExport(i, a.Field, externGlobal)

		if e == nil {
			return nil, false
		}

		t := i.obj.meta.globals[e.index].typ

		return func() []reflect.Value {
			bits := *(*uint64)(unsafe.Pointer(globalAddr(i, e.index)))

			return []reflect.Value{specBits(t, bits)}
		}, true
	}

	e := findExport(i, a.Field, externFunc)

	if e == nil {
		return nil, false
	}

	for _, t := range append(append([]ValType{}, e.params...), e.results...) {
		if t == V128 {
			return nil, false
		}
	}

	f, _ := i.export(e.name)
	args := []reflect.Value{reflect.ValueOf(i.handle())}

	if len(a.Args) != len(e.params) {
		return nil, false
	}

	for k, v := range a.Args {
		if e.params[k] == FuncRef && v.Type != "funcref" || e.params[k] == ExternRef && v.Type != "externref" {
			return nil, false
		}

		x, ok := specArg(i, v)

		if !ok || x.Type() != goType(e.params[k]) {
			return nil, false
		}

		args = append(args, x)
	}

	return func() []reflect.Value {
		out := f.Call(args)

		runtime.KeepAlive(i)

		return out
	}, true
}

func specBits(t ValType, bits uint64) reflect.Value {
	switch t {
	case I32:
		return reflect.ValueOf(uint32(bits))
	case F32:
		return reflect.ValueOf(math.Float32frombits(uint32(bits)))
	case F64:
		return reflect.ValueOf(math.Float64frombits(bits))
	case FuncRef, ExternRef:
		return reflect.ValueOf(uintptr(bits))
	}

	return reflect.ValueOf(bits)
}

func matches(i *Instance, got reflect.Value, want SpecValue) (bool, string) {
	var bits uint64

	switch x := got.Interface().(type) {
	case uint32:
		bits = uint64(x)
	case uint64:
		bits = x
	case float32:
		bits = uint64(math.Float32bits(x))
	case float64:
		bits = math.Float64bits(x)
	case uintptr:
		bits = uint64(x)
	}

	shown := fmt.Sprintf("%s %#x", want.Type, bits)

	var s string

	if len(want.Value) == 0 {
		return bits != 0, shown
	}

	if json.Unmarshal(want.Value, &s) != nil {
		return false, shown
	}

	switch {
	case s == "nan:canonical" && want.Type == "f32":
		return bits&0x7fffffff == 0x7fc00000, shown
	case s == "nan:canonical":
		return bits&0x7fffffffffffffff == 0x7ff8000000000000, shown
	case s == "nan:arithmetic" && want.Type == "f32":
		return bits&0x7fc00000 == 0x7fc00000, shown
	case s == "nan:arithmetic":
		return bits&0x7ff8000000000000 == 0x7ff8000000000000, shown
	case s == "null":
		return bits == 0, shown
	}

	x, err := strconv.ParseUint(s, 10, 64)

	if err != nil {
		return false, shown
	}

	if want.Type == "funcref" {
		ref, ok := i.specFunctionRef(x)

		return ok && bits == uint64(ref), shown
	}

	if want.Type == "externref" {
		x++
	}

	return bits == x, shown
}

func (r *SpecRunner) assertReturn(c *SpecCmd) {
	run, ok := r.invoke(c.Action)

	if !ok {
		r.skipped++

		return
	}

	for _, v := range c.Expected {
		if v.Type == "v128" {
			r.skipped++

			return
		}
	}

	var out []reflect.Value

	if code := trapOf(func() { out = run() }); code >= 0 {
		r.fail(c.Line, "trap %d", code)

		return
	}

	if c.Type == "assert_return" {
		if len(out) != len(c.Expected) {
			r.fail(c.Line, "%d results, expected %d", len(out), len(c.Expected))

			return
		}

		for k := range out {
			if ok, shown := matches(r.moduleOf(c.Action), out[k], c.Expected[k]); !ok {
				r.fail(c.Line, "result %d is %s, expected %s", k, shown, strings.Trim(string(c.Expected[k].Value), `"`))

				return
			}
		}
	}

	r.passed++
}

func (r *SpecRunner) assertTrap(c *SpecCmd) {
	if c.Action == nil {
		r.skipped++

		return
	}

	run, ok := r.invoke(c.Action)

	if !ok {
		r.skipped++

		return
	}

	want := -1

	if c.Type == "assert_exhaustion" {
		want = trapStack
	} else {
		for _, t := range specTraps {
			if strings.HasPrefix(c.Text, t.prefix) {
				want = int(t.code)
			}
		}
	}

	code := trapOf(func() { run() })

	switch {
	case code < 0:
		r.fail(c.Line, "no trap")
	case want >= 0 && code != want:
		r.fail(c.Line, "trap %d, expected %d", code, want)
	default:
		r.passed++
	}
}
