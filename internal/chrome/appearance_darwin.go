//go:build darwin

package chrome

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=10.14
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static void updateVisualEffectViews(NSView* view, NSAppearance* appearance, NSVisualEffectMaterial material) {
	if (view == nil) return;
	if ([view isKindOfClass:[NSVisualEffectView class]]) {
		NSVisualEffectView* ev = (NSVisualEffectView*)view;
		[ev setAppearance:appearance];
		[ev setMaterial:material];
		[ev setState:NSVisualEffectStateActive];
		[ev setBlendingMode:NSVisualEffectBlendingModeBehindWindow];
		[ev setNeedsDisplay:YES];
	}
	for (NSView* sub in [view subviews]) {
		updateVisualEffectViews(sub, appearance, material);
	}
}

static void updateWebViews(NSView* view) {
	if (view == nil) return;
	if ([view isKindOfClass:[WKWebView class]]) {
		WKWebView* wv = (WKWebView*)view;
		@try {
			[wv setValue:@NO forKey:@"drawsBackground"];
			[wv setValue:[NSColor clearColor] forKey:@"backgroundColor"];
		} @catch (NSException* e) {}
		if (@available(macOS 12.0, *)) {
			wv.underPageBackgroundColor = [NSColor clearColor];
		}
	}
	for (NSView* sub in [view subviews]) {
		updateWebViews(sub);
	}
}

static void compactTrafficLights(NSWindow* window) {
	if (window == nil) return;
	NSButton* closeBtn = [window standardWindowButton:NSWindowCloseButton];
	NSButton* minBtn = [window standardWindowButton:NSWindowMiniaturizeButton];
	NSButton* zoomBtn = [window standardWindowButton:NSWindowZoomButton];
	if (!closeBtn || !minBtn || !zoomBtn) return;

	CGFloat scale = 0.82;
	closeBtn.wantsLayer = YES;
	minBtn.wantsLayer = YES;
	zoomBtn.wantsLayer = YES;

	closeBtn.layer.affineTransform = CGAffineTransformMakeScale(scale, scale);
	minBtn.layer.affineTransform = CGAffineTransformMakeScale(scale, scale);
	zoomBtn.layer.affineTransform = CGAffineTransformMakeScale(scale, scale);

	NSRect fClose = closeBtn.frame;
	CGFloat y = fClose.origin.y;
	if ([closeBtn superview]) {
		CGFloat superH = [closeBtn superview].bounds.size.height;
		if (superH > 0) {
			y = superH - fClose.size.height - 10.0;
		} else {
			y = fClose.origin.y + 3.0;
		}
	} else {
		y = fClose.origin.y + 3.0;
	}
	CGFloat startX = 10.0;
	CGFloat gap = 15.0;

	[closeBtn setFrameOrigin:NSMakePoint(startX, y)];
	[minBtn setFrameOrigin:NSMakePoint(startX + gap, y)];
	[zoomBtn setFrameOrigin:NSMakePoint(startX + gap * 2, y)];
}

static void setupWindowNotifications(NSWindow* window) {
	if (window == nil) return;
	static void* observedWindow = NULL;
	if (observedWindow == (void*)window) return;
	observedWindow = (void*)window;

	void (^applyCompact)(NSNotification*) = ^(NSNotification *note) {
		compactTrafficLights(window);
		dispatch_async(dispatch_get_main_queue(), ^{
			compactTrafficLights(window);
		});
	};

	NSNotificationCenter* nc = [NSNotificationCenter defaultCenter];
	NSArray* notificationNames = @[
		NSWindowDidEndLiveResizeNotification,
		NSWindowDidResizeNotification,
		NSWindowDidBecomeKeyNotification,
		NSWindowDidResignKeyNotification,
		NSWindowDidBecomeMainNotification,
		NSWindowDidResignMainNotification,
		NSWindowDidExitFullScreenNotification,
		NSWindowDidEnterFullScreenNotification
	];

	for (NSNotificationName name in notificationNames) {
		[nc addObserverForName:name
						object:window
						 queue:[NSOperationQueue mainQueue]
					usingBlock:applyCompact];
	}
}

void setupWindowTransparency(void* ptr) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow* window = nil;
		if (ptr != NULL) {
			id obj = (id)ptr;
			if ([obj isKindOfClass:[NSWindow class]]) {
				window = (NSWindow*)obj;
			} else if ([obj respondsToSelector:@selector(window)]) {
				window = [obj window];
			}
		}
		if (window == nil) {
			window = [NSApp keyWindow] ?: [NSApp mainWindow];
		}
		if (window == nil) return;

		[window setOpaque:NO];
		[window setBackgroundColor:[NSColor clearColor]];
		if ([window contentView]) {
			updateWebViews([window contentView]);
			[[window contentView] setNeedsDisplay:YES];
		}
		compactTrafficLights(window);
		setupWindowNotifications(window);
		[window invalidateShadow];
	});
}

void setNativeAppearance(void* ptr, const char* mode) {
	NSString* modeStr = [NSString stringWithUTF8String:mode];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow* window = nil;
		if (ptr != NULL) {
			id obj = (id)ptr;
			if ([obj isKindOfClass:[NSWindow class]]) {
				window = (NSWindow*)obj;
			} else if ([obj respondsToSelector:@selector(window)]) {
				window = [obj window];
			}
		}
		if (window == nil) {
			window = [NSApp keyWindow] ?: [NSApp mainWindow];
			if (window == nil && [[NSApp windows] count] > 0) {
				window = [[NSApp windows] firstObject];
			}
		}

		if (window == nil) return;

		NSAppearance* appearance = nil;
		NSVisualEffectMaterial material = NSVisualEffectMaterialUnderWindowBackground;

		if ([modeStr isEqualToString:@"dark"]) {
			appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
		} else if ([modeStr isEqualToString:@"light"]) {
			appearance = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
		} else {
			appearance = nil;
		}

		[window setAppearance:appearance];
		[window setOpaque:NO];
		[window setBackgroundColor:[NSColor clearColor]];

		if ([window contentView]) {
			[[window contentView] setAppearance:appearance];
			updateVisualEffectViews([window contentView], appearance, material);
			updateWebViews([window contentView]);
			[[window contentView] setNeedsDisplay:YES];
		}
		compactTrafficLights(window);
		setupWindowNotifications(window);
		[window invalidateShadow];
	});
}
*/
import "C"
import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SetupNativeWindow 为无边框窗口设置原生透明与紧凑交通灯样式。
func SetupNativeWindow(window application.Window, _ []byte) {
	if window == nil {
		return
	}
	if wvWin, ok := window.(*application.WebviewWindow); ok {
		ptr := wvWin.NativeWindow()
		if ptr != nil {
			C.setupWindowTransparency(ptr)
		}
	}
}

// SetNativeWindowTheme 让 macOS 窗口跟随界面主题切换深浅外观。
func SetNativeWindowTheme(window application.Window, mode string) {
	if window == nil {
		return
	}
	var ptr unsafe.Pointer
	if wvWin, ok := window.(*application.WebviewWindow); ok {
		ptr = wvWin.NativeWindow()
	}
	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))
	C.setNativeAppearance(ptr, cMode)
}
