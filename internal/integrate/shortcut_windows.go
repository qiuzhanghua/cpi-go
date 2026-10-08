//go:build windows

package integrate

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// 开始菜单里的 .lnk 是二进制格式，只有 COM 的 IShellLink 能可靠地写出来。
//
// 为什么不用 PowerShell 的 WScript.Shell 代劳：那要给用户机器拉起一个子进程，
// 而「下载来的程序改 PATH、又拉起 PowerShell」正是杀软/EDR 的教科书特征。
// 这里用原生调用自己完成，外部看不到任何异常动作，也不引第三方依赖。
//
// 方法在 vtable 里的下标是 COM 规范定的，改不得。IShellLinkW 继承 IUnknown，
// 所以它自己的第一个方法从 3 开始；IPersistFile 是同一个对象上的另一个接口。
const (
	iUnknownQueryInterface = 0
	iUnknownRelease        = 2

	slGetPath         = 3
	slGetDescription  = 6
	slSetDescription  = 7
	slGetWorkingDir   = 8
	slSetWorkingDir   = 9
	slGetArguments    = 10
	slSetArguments    = 11
	slSetIconLocation = 17
	slSetPath         = 20

	pfLoad = 5
	pfSave = 6
)

var (
	ole32 = syscall.NewLazyDLL("ole32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

type winGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	// 00021401-0000-0000-C000-000000000046
	clsidShellLink = winGUID{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// 000214F9-0000-0000-C000-000000000046
	iidIShellLinkW = winGUID{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// 0000010B-0000-0000-C000-000000000046
	iidIPersistFile = winGUID{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
	slgpDefault             = 0
)

// comCall 按 vtable 下标调用一个 COM 方法。
//
// 接口指针全程以 unsafe.Pointer 形式传递（而不是 uintptr）：
// 一是 GC 认得出这是个指针；二是这样写出来的代码 go vet 的 unsafeptr 检查也过得去。
func comCall(p unsafe.Pointer, index int, args ...uintptr) uintptr {
	if p == nil {
		return 0x80004003 // E_POINTER
	}
	vtbl := *(*unsafe.Pointer)(p)
	fn := *(*uintptr)(unsafe.Pointer(uintptr(vtbl) + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(p))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	runtime.KeepAlive(p)
	return r
}

// coInit 初始化 COM。返回的函数负责配对的释放。
func coInit() (func(), error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	switch hr {
	case 0:
		return func() { procCoUninitialize.Call() }, nil
	case rpcEChangedMode:
		// 宿主线程已经以别的模式初始化过 COM 了，照样能用；只是不该由我们反初始化。
		return func() {}, nil
	default:
		return nil, fmt.Errorf("CoInitializeEx 失败（0x%08X）", uint32(hr))
	}
}

// newShellLink 造一个空的 IShellLink 对象，并返回释放函数。
func newShellLink() (unsafe.Pointer, func(), error) {
	release, err := coInit()
	if err != nil {
		return nil, nil, err
	}
	clsid := clsidShellLink
	iid := iidIShellLinkW
	var link unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&link)),
	)
	runtime.KeepAlive(clsid)
	runtime.KeepAlive(iid)
	runtime.KeepAlive(link)
	if hr != 0 || link == nil {
		release()
		return nil, nil, fmt.Errorf("CoCreateInstance(ShellLink) 失败（0x%08X）", uint32(hr))
	}
	return link, func() {
		comCall(link, iUnknownRelease)
		release()
	}, nil
}

func comSetString(p unsafe.Pointer, index int, s string) error {
	if s == "" {
		return nil
	}
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	hr := comCall(p, index, uintptr(unsafe.Pointer(&u[0])))
	runtime.KeepAlive(u)
	if hr != 0 {
		return fmt.Errorf("IShellLink 方法 #%d 失败（0x%08X）", index, uint32(hr))
	}
	return nil
}

// queryPersistFile 拿到同一个对象上的 IPersistFile —— 描述写完了，落盘要靠它。
func queryPersistFile(link unsafe.Pointer) (unsafe.Pointer, error) {
	var pf unsafe.Pointer
	iid := iidIPersistFile
	hr := comCall(link, iUnknownQueryInterface,
		uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&pf)))
	runtime.KeepAlive(iid)
	runtime.KeepAlive(pf)
	if hr != 0 || pf == nil {
		return nil, fmt.Errorf("拿不到 IPersistFile（0x%08X）", uint32(hr))
	}
	return pf, nil
}

func createShortcut(lnkPath string, sc Shortcut) error {
	link, done, err := newShellLink()
	if err != nil {
		return err
	}
	defer done()

	if err := comSetString(link, slSetPath, sc.Target); err != nil {
		return err
	}
	if err := comSetString(link, slSetArguments, sc.Arguments); err != nil {
		return err
	}
	if err := comSetString(link, slSetWorkingDir, sc.WorkingDir); err != nil {
		return err
	}
	if err := comSetString(link, slSetDescription, sc.Description); err != nil {
		return err
	}
	if sc.IconPath != "" {
		icon, err := syscall.UTF16FromString(sc.IconPath)
		if err != nil {
			return err
		}
		hr := comCall(link, slSetIconLocation,
			uintptr(unsafe.Pointer(&icon[0])), uintptr(sc.IconIndex))
		runtime.KeepAlive(icon)
		if hr != 0 {
			return fmt.Errorf("SetIconLocation 失败（0x%08X）", uint32(hr))
		}
	}

	pf, err := queryPersistFile(link)
	if err != nil {
		return err
	}
	defer comCall(pf, iUnknownRelease)

	file, err := syscall.UTF16FromString(lnkPath)
	if err != nil {
		return err
	}
	hr := comCall(pf, pfSave, uintptr(unsafe.Pointer(&file[0])), 1 /* fRemember */)
	runtime.KeepAlive(file)
	if hr != 0 {
		return fmt.Errorf("保存 .lnk 到 %s 失败（0x%08X）", lnkPath, uint32(hr))
	}
	return nil
}

// readShortcut 把一个 .lnk 读回来。
//
// 产品代码不用它。它是给测试做「写进去再读出来」的往返验证用的，而之所以
// 值得专门写一个读的实现：这段代码是手搓的 COM vtable 调用，下标写错一个
// 就会去调另一个方法 —— 编译照过、写文件也不一定报错。只有一个真的
// 「读回来和我写进去的一样」的测试，才能证明那些下标是对的。
func readShortcut(lnkPath string) (Shortcut, error) {
	var sc Shortcut

	link, done, err := newShellLink()
	if err != nil {
		return sc, err
	}
	defer done()

	pf, err := queryPersistFile(link)
	if err != nil {
		return sc, err
	}
	defer comCall(pf, iUnknownRelease)

	file, err := syscall.UTF16FromString(lnkPath)
	if err != nil {
		return sc, err
	}
	hr := comCall(pf, pfLoad, uintptr(unsafe.Pointer(&file[0])), 0 /* STGM_READ */)
	runtime.KeepAlive(file)
	if hr != 0 {
		return sc, fmt.Errorf("读不了 %s（0x%08X）", lnkPath, uint32(hr))
	}

	if sc.Target, err = comGetString(link, slGetPath, slgpDefault); err != nil {
		return sc, err
	}
	if sc.Arguments, err = comGetString(link, slGetArguments, 0); err != nil {
		return sc, err
	}
	if sc.WorkingDir, err = comGetString(link, slGetWorkingDir, 0); err != nil {
		return sc, err
	}
	if sc.Description, err = comGetString(link, slGetDescription, 0); err != nil {
		return sc, err
	}
	return sc, nil
}

// comGetString 调一个「把字符串写进调用方缓冲区」的 IShellLink 取值方法。
//
// GetPath 比另外三个多一个 fFlags 参数，多出来的那个由 extra 传。
func comGetString(p unsafe.Pointer, index int, extra uintptr) (string, error) {
	const bufLen = 1024
	buf := make([]uint16, bufLen)

	var hr uintptr
	if index == slGetPath {
		hr = comCall(p, index, uintptr(unsafe.Pointer(&buf[0])), uintptr(bufLen), 0, extra)
	} else {
		hr = comCall(p, index, uintptr(unsafe.Pointer(&buf[0])), uintptr(bufLen))
	}
	runtime.KeepAlive(buf)
	if hr != 0 {
		return "", fmt.Errorf("IShellLink 取值方法 #%d 失败（0x%08X）", index, uint32(hr))
	}
	return syscall.UTF16ToString(buf), nil
}
