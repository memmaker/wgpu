// Copyright 2025 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build darwin && !(js && wasm)

package metal

import (
	"testing"
	"unsafe"

	"github.com/go-webgpu/goffi/types"
)

// msgSendFixture returns an NSString "hello wgpu" and "wgpu" for calls that
// allocate nothing on the Objective-C side.
func msgSendFixture(tb testing.TB) (str, sub ID, done func()) {
	tb.Helper()
	if err := Init(); err != nil {
		tb.Skipf("Init: %v", err)
	}
	str, sub = NSString("hello wgpu"), NSString("wgpu")
	return str, sub, func() { Release(sub); Release(str) }
}

// BenchmarkMsgSend measures the Go side of an Objective-C call: no args,
// pointer args, and a struct argument with a struct result.
func BenchmarkMsgSend(b *testing.B) {
	str, sub, done := msgSendFixture(b)
	defer done()
	b.Run("noargs", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			MsgSendUint(str, Sel("length"))
		}
	})
	b.Run("ptrargs3", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			MsgSend(str, Sel("performSelector:withObject:withObject:"), uintptr(Sel("isEqual:")), uintptr(sub), uintptr(sub))
		}
	})
	b.Run("struct", func(b *testing.B) {
		b.ReportAllocs()
		var r NSRange
		for b.Loop() {
			_ = msgSend(str, Sel("rangeOfString:options:range:"), nsRangeType, unsafe.Pointer(&r),
				argPointer(uintptr(sub)), argUint64(0), argStruct(NSRange{Location: 0, Length: 10}, nsRangeType))
		}
	})
}

// TestMsgSendArgs checks argument and result passing for each argument kind.
func TestMsgSendArgs(t *testing.T) {
	str, sub, done := msgSendFixture(t)
	defer done()
	if n := MsgSendUint(str, Sel("length")); n != 10 {
		t.Fatalf("length = %d", n)
	}
	if !MsgSendBool(sub, Sel("isEqualToString:"), uintptr(sub)) || MsgSendBool(str, Sel("isEqualToString:"), uintptr(sub)) {
		t.Fatal("isEqualToString:")
	}
	if got := MsgSend(str, Sel("performSelector:withObject:withObject:"), uintptr(Sel("isEqual:")), uintptr(str), uintptr(sub)); got != 1 {
		t.Fatalf("performSelector:withObject:withObject: = %d, want 1", got)
	}
	for _, tc := range []struct {
		in, want NSRange
	}{
		{NSRange{0, 10}, NSRange{6, 4}},
		{NSRange{0, 9}, NSRange{0x7fffffffffffffff, 0}}, // NSNotFound
		{NSRange{6, 4}, NSRange{6, 4}},
	} {
		var r NSRange
		if err := msgSend(str, Sel("rangeOfString:options:range:"), nsRangeType, unsafe.Pointer(&r),
			argPointer(uintptr(sub)), argUint64(0), argStruct(tc.in, nsRangeType)); err != nil {
			t.Fatal(err)
		}
		if r != tc.want {
			t.Errorf("rangeOfString in %v = %v, want %v", tc.in, r, tc.want)
		}
	}
	// a bool and a float argument
	num := MsgSend(ID(GetClass("NSNumber")), Sel("alloc"))
	num = msgSendID(num, Sel("initWithDouble:"), argFloat64(2.5))
	defer Release(num)
	var d float64
	if err := msgSend(num, Sel("doubleValue"), types.DoubleTypeDescriptor, unsafe.Pointer(&d)); err != nil || d != 2.5 {
		t.Fatalf("doubleValue = %v (%v)", d, err)
	}
	b := MsgSend(ID(GetClass("NSNumber")), Sel("alloc"))
	b = msgSendID(b, Sel("initWithBool:"), argBool(true))
	defer Release(b)
	if !MsgSendBool(b, Sel("boolValue")) {
		t.Fatal("boolValue")
	}
}
