package desktop

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// Dynamic loading keeps go test / go run independent of a downloaded framework.
// These selectors are the pinned Sparkle 2 public Objective-C API.
@interface NSObject (RacomiSparkle)
- (id)initWithStartingUpdater:(BOOL)start updaterDelegate:(id)delegate userDriverDelegate:(id)driver;
- (id)updater;
- (BOOL)startUpdater:(NSError **)error;
- (void)checkForUpdates:(id)sender;
- (BOOL)canCheckForUpdates;
- (BOOL)automaticallyChecksForUpdates;
- (void)setAutomaticallyChecksForUpdates:(BOOL)on;
@end
extern void racomiPrepareUpdate(void);
extern void racomiUpdateAborted(void);
static id racomiUpdaterController;
static NSString *racomiUpdateError;
static NSString *racomiTrustedFeed;
static void (^racomiInstallHandler)(void);
@interface RacomiUpdaterDelegate : NSObject
@end
@implementation RacomiUpdaterDelegate
// Launch Services does not preserve these profile environments. An explicit
// profile can install and exit, then be started again with its original profile.
- (BOOL)updaterShouldRelaunchApplication:(id)updater { return getenv("RACOMI_DATA_DIR") == NULL && getenv("CFFIXED_USER_HOME") == NULL; }
- (void)updater:(id)updater didAbortWithError:(NSError *)error { if(racomiInstallHandler) { [racomiInstallHandler release]; racomiInstallHandler=nil; } racomiUpdateAborted(); }
- (NSString *)feedURLStringForUpdater:(id)updater { return racomiTrustedFeed; }
- (BOOL)updater:(id)updater shouldPostponeRelaunchForUpdate:(id)item untilInvokingBlock:(void (^)(void))handler {
 if (racomiInstallHandler) { [racomiInstallHandler release]; }
 racomiInstallHandler = [handler copy];
 racomiPrepareUpdate();
 return YES;
}
@end
static RacomiUpdaterDelegate *racomiUpdaterDelegate;

static void racomiStartUpdater(const char *identity, const char *version, const char *commit, const char *feed, const char *key) {
 if (racomiUpdaterController) return;
 NSBundle *app = NSBundle.mainBundle;
 NSDictionary *info = app.infoDictionary;
 NSString *path = app.bundlePath.stringByResolvingSymlinksInPath;
 NSString *userApps = [NSHomeDirectory() stringByAppendingPathComponent:@"Applications/"];
 BOOL installed = [path hasPrefix:@"/Applications/"] || [path hasPrefix:userApps];
 if (!installed || [path containsString:@"/AppTranslocation/"]) {
  racomiUpdateError = @"Move the official app to Applications before enabling updates."; return;
 }
 if (![app.bundleIdentifier isEqualToString:@(identity)] || ![info[@"RacomiChannel"] isEqualToString:@"stable"] ||
     ![info[@"RacomiExecutor"] isEqualToString:@"ci"] || ![info[@"RacomiReleaseVersion"] isEqualToString:@(version)] ||
     ![info[@"RacomiCommit"] isEqualToString:@(commit)] || ![info[@"SUFeedURL"] isEqualToString:@(feed)] ||
     ![info[@"SUPublicEDKey"] isEqualToString:@(key)]) {
  racomiUpdateError = @"The installed app does not match its release identity."; return;
 }
 NSError *error = nil;
 NSBundle *framework = [NSBundle bundleWithPath:[app.privateFrameworksPath stringByAppendingPathComponent:@"Sparkle.framework"]];
 if (!framework || ![framework loadAndReturnError:&error]) {
  racomiUpdateError = @"The Sparkle update framework could not be loaded."; return;
 }
 Class controllerClass = NSClassFromString(@"SPUStandardUpdaterController");
 if (!controllerClass) { racomiUpdateError = @"The Sparkle update framework could not be loaded."; return; }
 racomiTrustedFeed = [@(feed) copy];
 racomiUpdaterDelegate = [RacomiUpdaterDelegate new];
 racomiUpdaterController = [[controllerClass alloc] initWithStartingUpdater:NO updaterDelegate:racomiUpdaterDelegate userDriverDelegate:nil];
 if (![[racomiUpdaterController updater] startUpdater:&error]) {
  racomiUpdateError = [[error localizedDescription] copy];
  [racomiUpdaterController release]; racomiUpdaterController = nil;
 }
}
static char *racomiUpdaterStatus(void) {
 id updater = [racomiUpdaterController updater];
 NSDictionary *status = @{@"available":@(updater != nil), @"canCheck":@([updater canCheckForUpdates] || racomiInstallHandler != nil),
  @"automatic":@([updater automaticallyChecksForUpdates]), @"reason":racomiUpdateError ?: @""};
 NSString *json = [[[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:status options:0 error:nil] encoding:NSUTF8StringEncoding] autorelease];
 return strdup(json.UTF8String);
}
static void racomiCheckUpdates(void) {
 if (racomiInstallHandler) { racomiPrepareUpdate(); return; }
 [racomiUpdaterController checkForUpdates:nil];
}
void racomiResumeUpdate(void) {
 if (racomiInstallHandler) {
  void (^handler)(void) = racomiInstallHandler;
  racomiInstallHandler = nil;
  handler(); [handler release];
 }
}
static void racomiSetAutomaticUpdates(BOOL on) { [[racomiUpdaterController updater] setAutomaticallyChecksForUpdates:on]; }
*/
import "C"

import (
	"encoding/json"
	"racomi/internal/buildinfo"
	"unsafe"
)

var updaterService *Service // Set once on the UI thread before Sparkle starts.

func nativeStartUpdater(s *Service) {
	updaterService = s
	info := buildinfo.Current()
	values := []string{info.ID, info.Version, info.Commit, buildinfo.FeedURL, buildinfo.PublicKey}
	args := make([]*C.char, len(values))
	for i, v := range values {
		args[i] = C.CString(v)
		defer C.free(unsafe.Pointer(args[i]))
	}
	C.racomiStartUpdater(args[0], args[1], args[2], args[3], args[4])
}
func nativeUpdaterState() UpdaterState {
	raw := C.racomiUpdaterStatus()
	defer C.free(unsafe.Pointer(raw))
	var state UpdaterState
	if err := json.Unmarshal([]byte(C.GoString(raw)), &state); err != nil {
		state.Reason = "The desktop updater is unavailable."
	}
	return state
}
func nativeCheckUpdates()               { C.racomiCheckUpdates() }
func nativeSetAutomaticUpdates(on bool) { C.racomiSetAutomaticUpdates(C.BOOL(on)) }
