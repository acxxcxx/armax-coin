//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const (
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmSetIcon       = 0x0080
	wmContextMenu   = 0x007B
	wmCommand       = 0x0111
	wmSysCommand    = 0x0112
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmExitSizeMove  = 0x0232
	wmApp           = 0x8000
	wmTrayCallback  = wmApp + 101

	sizeMinimized      = 1
	wsOverlappedWindow = 0x00CF0000
	swHide             = 0
	swShow             = 5
	swRestore          = 9
	scMinimize         = 0xF020
	idcArrow           = 32512
	smCxScreen         = 0
	smCyScreen         = 1
	swpNoMove          = 0x0002
	swpNoZOrder        = 0x0004
	iconSmall          = 0
	iconBig            = 1
	coinitAptThreaded  = 0x2
	mbOK               = 0x00000000
	mbIconError        = 0x00000010
	mbSystemModal      = 0x00001000

	trayIconUID = 1
	nimAdd      = 0x00000000
	nimModify   = 0x00000001
	nimDelete   = 0x00000002
	nifMessage  = 0x00000001
	nifIcon     = 0x00000002
	nifTip      = 0x00000004
	nifInfo     = 0x00000010
	niifInfo    = 0x00000001

	mfString       = 0x00000000
	mfGrayed       = 0x00000001
	mfDisabled     = 0x00000002
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	cmdTrayToggle = 4001
	cmdTrayExit   = 4002
	cmdTrayStatus = 4003
)

var (
	user32                        = windows.NewLazySystemDLL("user32.dll")
	shell32                       = windows.NewLazySystemDLL("shell32.dll")
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	ole32                         = windows.NewLazySystemDLL("ole32.dll")
	procRegisterClassExW          = user32.NewProc("RegisterClassExW")
	procUnregisterClassW          = user32.NewProc("UnregisterClassW")
	procCreateWindowExW           = user32.NewProc("CreateWindowExW")
	procShowWindow                = user32.NewProc("ShowWindow")
	procUpdateWindow              = user32.NewProc("UpdateWindow")
	procDestroyWindow             = user32.NewProc("DestroyWindow")
	procPostQuitMessage           = user32.NewProc("PostQuitMessage")
	procPostMessageW              = user32.NewProc("PostMessageW")
	procGetMessageW               = user32.NewProc("GetMessageW")
	procTranslateMessage          = user32.NewProc("TranslateMessage")
	procDispatchMessageW          = user32.NewProc("DispatchMessageW")
	procDefWindowProcW            = user32.NewProc("DefWindowProcW")
	procGetSystemMetrics          = user32.NewProc("GetSystemMetrics")
	procLoadCursorW               = user32.NewProc("LoadCursorW")
	procSendMessageW              = user32.NewProc("SendMessageW")
	procGetWindowRect             = user32.NewProc("GetWindowRect")
	procSetWindowPos              = user32.NewProc("SetWindowPos")
	procGetKeyState               = user32.NewProc("GetKeyState")
	procCreateIconFromResourceEx  = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon               = user32.NewProc("DestroyIcon")
	procMessageBoxW               = user32.NewProc("MessageBoxW")
	procSetProcessDpiAwarenessCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware        = user32.NewProc("SetProcessDPIAware")
	procRegisterWindowMessageW    = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu           = user32.NewProc("CreatePopupMenu")
	procAppendMenuW               = user32.NewProc("AppendMenuW")
	procTrackPopupMenu            = user32.NewProc("TrackPopupMenu")
	procDestroyMenu               = user32.NewProc("DestroyMenu")
	procSetForegroundWindow       = user32.NewProc("SetForegroundWindow")
	procGetCursorPos              = user32.NewProc("GetCursorPos")
	procIsWindowVisible           = user32.NewProc("IsWindowVisible")
	procSetMenuDefaultItem        = user32.NewProc("SetMenuDefaultItem")
	procShellNotifyIconW          = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW          = kernel32.NewProc("GetModuleHandleW")
	procCoInitializeEx            = ole32.NewProc("CoInitializeEx")
	procCoUninitialize            = ole32.NewProc("CoUninitialize")
)

type winWndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winPoint struct {
	X, Y int32
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

type winMsg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}

type winGuid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type winNotifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutVersion  uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         winGuid
	HBalloonIcon     uintptr
}

func desktopUsesEmbeddedWindow() bool {
	return true
}

func copyUTF16Fixed(dst []uint16, s string) {
	if len(dst) == 0 {
		return
	}
	for i := range dst {
		dst[i] = 0
	}
	encoded := utf16.Encode([]rune(s))
	limit := len(dst) - 1
	if len(encoded) > limit {
		encoded = encoded[:limit]
	}
	copy(dst, encoded)
	dst[len(encoded)] = 0
}

func buildTrayStatusText() string {
	st := getNodeBootstrapStatus()
	nodeLabel := "Offline"
	switch strings.ToLower(strings.TrimSpace(st.State)) {
	case "online":
		nodeLabel = "Online"
	case "starting":
		nodeLabel = "Starting"
	case "error":
		nodeLabel = "Error"
	default:
		if atomic.LoadInt64(&lastTipHeight) > 0 {
			nodeLabel = "Online"
		}
	}

	miningLock.Lock()
	mActive := miningActive
	miningLock.Unlock()

	minerLabel := guiMinerStatusLabel(mActive)
	return fmt.Sprintf("Node: %s | Miner: %s", nodeLabel, minerLabel)
}

func shellNotifyAddTrayIcon(hwnd, hIcon uintptr, tooltip string) bool {
	var nid winNotifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = trayIconUID
	nid.UFlags = nifMessage | nifTip
	if hIcon != 0 {
		nid.UFlags |= nifIcon
		nid.HIcon = hIcon
	}
	nid.UCallbackMessage = wmTrayCallback
	copyUTF16Fixed(nid.SzTip[:], tooltip)
	ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	return ret != 0
}

func shellNotifyBalloon(hwnd uintptr, title, text string) {
	var nid winNotifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = trayIconUID
	nid.UFlags = nifInfo
	nid.DwInfoFlags = niifInfo
	copyUTF16Fixed(nid.SzInfoTitle[:], title)
	copyUTF16Fixed(nid.SzInfo[:], text)
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func shellNotifyDeleteTrayIcon(hwnd uintptr) {
	var nid winNotifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = trayIconUID
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func appendTrayMenuItem(hMenu uintptr, flags uintptr, id uintptr, label string) {
	if flags&mfSeparator != 0 {
		_, _, _ = procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
		return
	}
	txtPtr, _ := windows.UTF16PtrFromString(label)
	_, _, _ = procAppendMenuW.Call(hMenu, flags, id, uintptr(unsafe.Pointer(txtPtr)))
	runtime.KeepAlive(txtPtr)
}

func showNativeErrorBox(title, message string) {
	tPtr, _ := windows.UTF16PtrFromString(title)
	mPtr, _ := windows.UTF16PtrFromString(message)
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(mPtr)), uintptr(unsafe.Pointer(tPtr)), mbOK|mbIconError|mbSystemModal)
}

func createIconFromICOBytes(ico []byte, desiredW, desiredH int) uintptr {
	if len(ico) < 6 {
		return 0
	}
	reserved := binary.LittleEndian.Uint16(ico[0:2])
	imgType := binary.LittleEndian.Uint16(ico[2:4])
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	if reserved != 0 || imgType != 1 || count <= 0 || len(ico) < 6+count*16 {
		return 0
	}

	bestIdx := -1
	bestScore := int(1 << 30)
	bestBits := -1
	for i := 0; i < count; i++ {
		base := 6 + i*16
		w := int(ico[base])
		if w == 0 {
			w = 256
		}
		h := int(ico[base+1])
		if h == 0 {
			h = 256
		}
		bitCount := int(binary.LittleEndian.Uint16(ico[base+6 : base+8]))
		bytesInRes := int(binary.LittleEndian.Uint32(ico[base+8 : base+12]))
		imageOffset := int(binary.LittleEndian.Uint32(ico[base+12 : base+16]))
		if bytesInRes <= 0 || imageOffset < 6+count*16 || imageOffset+bytesInRes > len(ico) {
			continue
		}
		dw := w - desiredW
		if dw < 0 {
			dw = -dw
		}
		dh := h - desiredH
		if dh < 0 {
			dh = -dh
		}
		score := dw + dh
		if score < bestScore || (score == bestScore && bitCount > bestBits) {
			bestScore = score
			bestBits = bitCount
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return 0
	}

	base := 6 + bestIdx*16
	bytesInRes := int(binary.LittleEndian.Uint32(ico[base+8 : base+12]))
	imageOffset := int(binary.LittleEndian.Uint32(ico[base+12 : base+16]))
	hIcon, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&ico[imageOffset])),
		uintptr(bytesInRes),
		1,          // fIcon = TRUE
		0x00030000, // default Windows icon version
		uintptr(desiredW),
		uintptr(desiredH),
		0,
	)
	return hIcon
}

func destroyHIcon(h uintptr) {
	if h != 0 {
		_, _, _ = procDestroyIcon.Call(h)
	}
}

func isCtrlPressed() bool {
	st, _, _ := procGetKeyState.Call(0x11) // VK_CONTROL
	return (uint16(st) & 0x8000) != 0
}

func runDesktopWindow(guiURL string, launchSecret string, windowTitle string, exitCh <-chan struct{}, onClosed func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if procSetProcessDpiAwarenessCtx.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
		_, _, _ = procSetProcessDpiAwarenessCtx.Call(^uintptr(3))
	} else {
		_, _, _ = procSetProcessDPIAware.Call()
	}

	_, _, _ = procCoInitializeEx.Call(0, coinitAptThreaded)
	defer func() {
		_, _, _ = procCoUninitialize.Call()
	}()

	ver, verErr := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if verErr != nil || strings.TrimSpace(ver) == "" {
		showNativeErrorBox(
			"Armaxcoin Core Wallet",
			"Microsoft Edge WebView2 Runtime is required to run the standalone Armaxcoin desktop wallet.\n\nPlease install Microsoft Edge WebView2 Runtime (Evergreen) and launch armgui.exe again.",
		)
		return fmt.Errorf("WebView2 runtime unavailable: %v", verErr)
	}

	tmpDir, err := os.MkdirTemp("", "armax-wv2-*")
	if err != nil {
		return fmt.Errorf("create WebView2 profile dir: %w", err)
	}
	defer func() {
		for i := 0; i < 6; i++ {
			if os.RemoveAll(tmpDir) == nil {
				break
			}
			time.Sleep(120 * time.Millisecond)
		}
	}()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	hIconBig := createIconFromICOBytes(armaxLogoICO, 32, 32)
	hIconSmall := createIconFromICOBytes(armaxLogoICO, 16, 16)
	defer destroyHIcon(hIconBig)
	defer destroyHIcon(hIconSmall)
	trayIcon := hIconSmall
	if trayIcon == 0 {
		trayIcon = hIconBig
	}

	taskbarCreatedName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	tcRet, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(taskbarCreatedName)))
	taskbarCreatedMsg := uint32(tcRet)
	runtime.KeepAlive(taskbarCreatedName)

	className := fmt.Sprintf("ArmaxcoinCoreWindow_%d", time.Now().UnixNano())
	classNamePtr, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return err
	}
	titlePtr, err := windows.UTF16PtrFromString(windowTitle)
	if err != nil {
		return err
	}

	var mu sync.Mutex
	var chromium *edge.Chromium
	var shuttingDown bool
	var trayAdded bool
	var trayTipShown bool

	ensureTrayIcon := func(hwnd uintptr) bool {
		if shellNotifyAddTrayIcon(hwnd, trayIcon, "Armaxcoin Core — Desktop Wallet") {
			mu.Lock()
			trayAdded = true
			mu.Unlock()
			return true
		}
		return false
	}

	removeTrayIcon := func(hwnd uintptr) {
		mu.Lock()
		wasAdded := trayAdded
		trayAdded = false
		mu.Unlock()
		if wasAdded {
			shellNotifyDeleteTrayIcon(hwnd)
		}
	}

	minimizeToTray := func(hwnd uintptr) bool {
		mu.Lock()
		down := shuttingDown
		added := trayAdded
		tipShown := trayTipShown
		mu.Unlock()
		if down {
			return false
		}
		if !added {
			added = ensureTrayIcon(hwnd)
			if !added {
				return false
			}
		}
		_, _, _ = procShowWindow.Call(hwnd, swHide)
		if !tipShown {
			mu.Lock()
			trayTipShown = true
			mu.Unlock()
			shellNotifyBalloon(hwnd, "Armaxcoin Core",
				"Minimized to system tray. Node & wallet remain running in the background. Click the tray icon to restore.")
		}
		return true
	}

	restoreFromTray := func(hwnd uintptr) {
		mu.Lock()
		c := chromium
		down := shuttingDown
		mu.Unlock()
		_, _, _ = procShowWindow.Call(hwnd, swShow)
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		_, _, _ = procUpdateWindow.Call(hwnd)
		if !down && c != nil && c.GetController() != nil {
			_ = c.Show()
			c.Resize()
			c.Focus()
		}
	}

	toggleWindowVisibility := func(hwnd uintptr) {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis != 0 {
			_ = minimizeToTray(hwnd)
		} else {
			restoreFromTray(hwnd)
		}
	}

	showTrayContextMenu := func(hwnd uintptr) {
		hMenu, _, _ := procCreatePopupMenu.Call()
		if hMenu == 0 {
			return
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		toggleLabel := "Open Armaxcoin Wallet"
		if vis != 0 {
			toggleLabel = "Minimize to Tray"
		}
		appendTrayMenuItem(hMenu, mfString, cmdTrayToggle, toggleLabel)
		_, _, _ = procSetMenuDefaultItem.Call(hMenu, cmdTrayToggle, 0)
		appendTrayMenuItem(hMenu, mfSeparator, 0, "")
		appendTrayMenuItem(hMenu, mfString|mfGrayed|mfDisabled, cmdTrayStatus, buildTrayStatusText())
		appendTrayMenuItem(hMenu, mfSeparator, 0, "")
		appendTrayMenuItem(hMenu, mfString, cmdTrayExit, "Exit Armaxcoin Wallet")

		var pt winPoint
		_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		_, _, _ = procTrackPopupMenu.Call(hMenu, tpmBottomAlign|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
		_, _, _ = procPostMessageW.Call(hwnd, wmNull, 0, 0)
		_, _, _ = procDestroyMenu.Call(hMenu)
	}

	wndProc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg {
			mu.Lock()
			down := shuttingDown
			mu.Unlock()
			if !down {
				_ = ensureTrayIcon(hwnd)
			}
			return 0
		}
		switch msg {
		case wmSize:
			if wParam == sizeMinimized {
				return 0
			}
			mu.Lock()
			c := chromium
			down := shuttingDown
			mu.Unlock()
			if !down && c != nil && c.GetController() != nil {
				_ = c.Show()
				c.Resize()
			}
			return 0
		case wmSysCommand:
			if (wParam & 0xFFF0) == scMinimize {
				if minimizeToTray(hwnd) {
					return 0
				}
			}
		case wmTrayCallback:
			switch uint32(lParam & 0xFFFF) {
			case wmLButtonUp, wmLButtonDblClk:
				restoreFromTray(hwnd)
				return 0
			case wmRButtonUp, wmContextMenu:
				showTrayContextMenu(hwnd)
				return 0
			}
		case wmCommand:
			switch uint32(wParam & 0xFFFF) {
			case cmdTrayToggle:
				toggleWindowVisibility(hwnd)
				return 0
			case cmdTrayExit:
				_, _, _ = procPostMessageW.Call(hwnd, wmClose, 0, 0)
				return 0
			}
		case wmMove:
			mu.Lock()
			c := chromium
			down := shuttingDown
			mu.Unlock()
			if !down && c != nil && c.GetController() != nil {
				_ = c.NotifyParentWindowPositionChanged()
			}
			return 0
		case wmSetFocus:
			mu.Lock()
			c := chromium
			down := shuttingDown
			mu.Unlock()
			if !down && c != nil && c.GetController() != nil {
				_ = c.Show()
				c.Focus()
			}
			return 0
		case wmExitSizeMove:
			var rc winRect
			ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
			if ret != 0 {
				w := rc.Right - rc.Left
				h := rc.Bottom - rc.Top
				minW := int32(960)
				minH := int32(680)
				if w < minW || h < minH {
					if w < minW {
						w = minW
					}
					if h < minH {
						h = minH
					}
					_, _, _ = procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(w), uintptr(h), swpNoMove|swpNoZOrder)
				}
			}
			return 0
		case wmClose:
			mu.Lock()
			shuttingDown = true
			c := chromium
			mu.Unlock()
			if c != nil {
				c.ShuttingDown()
			}
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			removeTrayIcon(hwnd)
			_, _, _ = procPostQuitMessage.Call(0)
			return 0
		}
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})

	wc := winWndClassExW{
		cbSize:        uint32(unsafe.Sizeof(winWndClassExW{})),
		style:         0x0003, // CS_HREDRAW | CS_VREDRAW
		lpfnWndProc:   wndProc,
		hInstance:     hInst,
		hIcon:         hIconBig,
		hCursor:       hCursor,
		hbrBackground: 6, // COLOR_WINDOW + 1
		lpszClassName: classNamePtr,
		hIconSm:       hIconSmall,
	}
	atom, _, regErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("RegisterClassExW failed: %w", regErr)
	}
	defer func() {
		_, _, _ = procUnregisterClassW.Call(uintptr(unsafe.Pointer(classNamePtr)), hInst)
	}()

	winW := int32(1240)
	winH := int32(860)
	scrW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	scrH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	if int32(scrW) > 0 && winW > int32(scrW)-40 {
		winW = int32(scrW) - 40
	}
	if int32(scrH) > 0 && winH > int32(scrH)-60 {
		winH = int32(scrH) - 60
	}
	posX := (int32(scrW) - winW) / 2
	posY := (int32(scrH) - winH) / 2
	if posX < 0 {
		posX = 0
	}
	if posY < 0 {
		posY = 0
	}

	hwnd, _, createErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		wsOverlappedWindow,
		uintptr(posX),
		uintptr(posY),
		uintptr(winW),
		uintptr(winH),
		0,
		0,
		hInst,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", createErr)
	}
	if hIconBig != 0 {
		_, _, _ = procSendMessageW.Call(hwnd, wmSetIcon, iconBig, hIconBig)
	}
	if hIconSmall != 0 {
		_, _, _ = procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, hIconSmall)
	}
	_ = ensureTrayIcon(hwnd)
	defer removeTrayIcon(hwnd)

	c := edge.NewChromium()
	c.DataPath = tmpDir
	c.SetErrorCallback(func(cbErr error) {
		fmt.Fprintf(os.Stderr, "[WebView2] %v\n", cbErr)
	})
	c.AcceleratorKeyCallback = func(vk uint) bool {
		// Block browser DevTools, full reload, view-source, and print hotkeys; allow clipboard editing shortcuts.
		switch vk {
		case 0x74, 0x7B: // VK_F5, VK_F12
			return true
		case 'R', 'U', 'I', 'J', 'P':
			if isCtrlPressed() {
				return true
			}
		}
		return false
	}

	mu.Lock()
	chromium = c
	mu.Unlock()

	_, _, _ = procShowWindow.Call(hwnd, swShow)
	_, _, _ = procUpdateWindow.Call(hwnd)

	if !c.Embed(hwnd) {
		_, _, _ = procDestroyWindow.Call(hwnd)
		return fmt.Errorf("failed to embed WebView2 controller")
	}

	if settings, err := c.GetSettings(); err == nil && settings != nil {
		_ = settings.PutIsScriptEnabled(true)
		_ = settings.PutAreDefaultScriptDialogsEnabled(true)
		_ = settings.PutAreDevToolsEnabled(false)
		_ = settings.PutAreDefaultContextMenusEnabled(false)
		_ = settings.PutIsStatusBarEnabled(false)
		_ = settings.PutIsZoomControlEnabled(false)
		_ = settings.PutAreHostObjectsAllowed(false)
		if s3 := settings.GetICoreWebView2Settings3(); s3 != nil {
			_, _, _ = s3.Vtbl.PutAreBrowserAcceleratorKeysEnabled.Call(uintptr(unsafe.Pointer(s3)), 0)
		}
	}
	_ = c.PutIsPasswordAutosaveEnabled(false)
	_ = c.PutIsGeneralAutofillEnabled(false)
	_ = c.AllowExternalDrag(false)
	if ctrl := c.GetController(); ctrl != nil {
		if ctrl2 := ctrl.GetICoreWebView2Controller2(); ctrl2 != nil {
			c.SetBackgroundColour(248, 250, 252, 255)
		}
	}

	guiOrigin := strings.TrimRight(guiURL, "/")
	c.AddWebResourceRequestedFilter("*", edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)
	c.WebResourceRequestedCallback = func(req *edge.ICoreWebView2WebResourceRequest, _ *edge.ICoreWebView2WebResourceRequestedEventArgs) {
		uri, uriErr := req.GetUri()
		if uriErr != nil {
			return
		}
		if uri == guiOrigin || strings.HasPrefix(uri, guiOrigin+"/") {
			if hdrs, hdrErr := req.GetHeaders(); hdrErr == nil && hdrs != nil {
				_ = hdrs.SetHeader("X-Armx-Launch-Auth", launchSecret)
				_ = hdrs.Release()
			}
		}
	}
	c.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		_ = c.Show()
		c.Resize()
	}

	_ = c.Show()
	c.Resize()
	c.Navigate(guiOrigin + "/auth/launch?ticket=" + launchSecret)
	_, _, _ = procShowWindow.Call(hwnd, swShow)
	_, _, _ = procUpdateWindow.Call(hwnd)
	c.Focus()

	windowDone := make(chan struct{})
	if exitCh != nil {
		go func() {
			select {
			case <-exitCh:
				_, _, _ = procPostMessageW.Call(hwnd, wmClose, 0, 0)
			case <-windowDone:
			}
		}()
	}

	var msg winMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	close(windowDone)
	if onClosed != nil {
		onClosed()
	}
	return nil
}
