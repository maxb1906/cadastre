//go:build windows

package clean

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Recycle bin through IFileOperation (the Shell API used by Explorer), so
// that the user can restore what Cadastre deleted.

var (
	ole32                           = windows.NewLazySystemDLL("ole32.dll")
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoUninitialize              = ole32.NewProc("CoUninitialize")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOperation = windows.GUID{Data1: 0x3ad05575, Data2: 0x8857, Data3: 0x4850, Data4: [8]byte{0x92, 0x77, 0x11, 0xb8, 0x5b, 0xdb, 0x8e, 0x09}}
	iidIFileOperation  = windows.GUID{Data1: 0x947aab5f, Data2: 0x0a5c, Data3: 0x4c13, Data4: [8]byte{0xb4, 0xd6, 0x4b, 0xf7, 0x83, 0x6f, 0xc9, 0xf8}}
	iidIShellItem      = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxAll               = 0x17

	fofSilent           = 0x0004
	fofNoConfirmation   = 0x0010
	fofAllowUndo        = 0x0040
	fofNoErrorUI        = 0x0400
	fofxRecycleOnDelete = 0x00080000 // Windows 8+
	fofxEarlyFailure    = 0x00100000

	// IFileOperation vtable slots (after IUnknown's 3).
	vtRelease                 = 2
	vtSetOperationFlags       = 5
	vtDeleteItem              = 18
	vtPerformOperations       = 21
	vtGetAnyOperationsAborted = 22
)

// vcall calls method slot of a COM interface pointer.
func vcall(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func hr(r uintptr) error {
	if int32(r) < 0 {
		return fmt.Errorf("HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func recycle(paths []string) map[string]error {
	out := map[string]error{}
	if len(paths) == 0 {
		return out
	}
	fail := func(err error) map[string]error {
		for _, p := range paths {
			out[p] = fmt.Errorf("%w: %v", ErrNoRecycle, err)
		}
		return out
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(r) >= 0 {
		defer procCoUninitialize.Call()
	}
	var op unsafe.Pointer
	if err := hr(func() uintptr {
		r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, clsctxAll,
			uintptr(unsafe.Pointer(&iidIFileOperation)), uintptr(unsafe.Pointer(&op)))
		return r
	}()); err != nil {
		return fail(err)
	}
	defer vcall(op, vtRelease)
	if err := hr(vcall(op, vtSetOperationFlags, fofAllowUndo|fofNoConfirmation|fofSilent|fofNoErrorUI|fofxRecycleOnDelete|fofxEarlyFailure)); err != nil {
		return fail(err)
	}
	queued := 0
	for _, p := range paths {
		var item unsafe.Pointer
		r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(utf16(p))), 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item)))
		if err := hr(r); err != nil {
			out[p] = fmt.Errorf("%w: %v (path too long for the Shell? use permanent mode)", ErrNoRecycle, err)
			continue
		}
		if err := hr(vcall(op, vtDeleteItem, uintptr(item), 0)); err != nil {
			out[p] = err
		} else {
			queued++
		}
		vcall(item, vtRelease)
	}
	if queued > 0 {
		perr := hr(vcall(op, vtPerformOperations))
		var aborted int32
		vcall(op, vtGetAnyOperationsAborted, uintptr(unsafe.Pointer(&aborted)))
		// Per-item outcome: what still exists was not recycled.
		for _, p := range paths {
			if out[p] != nil {
				continue
			}
			if _, err := (OSFS{}).Stat(p); err == nil {
				e := errors.New("not recycled")
				if perr != nil {
					e = perr
				}
				out[p] = e
			}
		}
	}
	return out
}
