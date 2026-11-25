(module
  (import "kernel" "strcasecmp" (func $compare (param i32 i32) (result i32)))
  (import "kernel" "strndup" (func $duplicate (param i32 i32) (result i32)))
  (import "kernel" "write" (func $write (param i32 i32 i32) (result i32)))
  (import "kernel" "sigaction" (func $action (param i32 i32 i32 i32 i32 i32) (result i32)))
  (memory 32833 32833)
  (global (export "__stack_pointer") (mut i32) (i32.const 32768))
  (global (export "__heap_base") i32 (i32.const 65536))
  (global $handled (mut i32) (i32.const 0))
  (data (i32.const 2048) "R")
  ;; A struct sigaction of 24 bytes: the handler, 1, restarting what it
  ;; interrupts; sigaction takes its size and a siginfo_t's, 44.
  (data (i32.const 2056) "\01\00\00\00\00\00\00\00\00\00\00\00\00\00\00\00\00\00\00\10\00\00\00\00")

  (func (export "__call_signal_handler") (param $handler i32) (param $number i32)
    (if (i32.ne (local.get $handler) (i32.const 1)) (then unreachable))
    (if (i32.ne (local.get $number) (i32.const 10)) (then unreachable))
    (global.set $handled (i32.const 1)))

  (func (export "_start")
    (if (i32.ne (call $action (i32.const 10) (i32.const 2056) (i32.const 24) (i32.const 0) (i32.const 24) (i32.const 44)) (i32.const 0)) (then unreachable))
    ;; Keep the stack, static results and signal info below the input.
    ;; 2^31 nonzero bytes plus NUL require 2^31 + 1 bytes to copy.
    (memory.fill (i32.const 4194304) (i32.const 1) (i32.const 2147483648))
    (i32.store8 (i32.const 2151677952) (i32.const 0))
    (if (i32.ne (call $write (i32.const 1) (i32.const 2048) (i32.const 1)) (i32.const 1)) (then unreachable))
    ;; The host's signals come during a long call of the kernel, a byte's
    ;; comparison at a time, and are delivered after it.
    (if (i32.ne (call $compare (i32.const 4194304) (i32.const 4194304)) (i32.const 0)) (then unreachable))
    (if (i32.ne (global.get $handled) (i32.const 1)) (then unreachable))
    (if (i32.ne (call $duplicate (i32.const 4194304) (i32.const -1)) (i32.const 0)) (then unreachable))
    (if (i32.ne (i32.load (i32.const 0)) (i32.const 12)) (then unreachable)))
)
