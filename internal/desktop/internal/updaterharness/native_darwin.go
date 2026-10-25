// Package updaterharness inspects the production Objective-C delegate without
// starting Sparkle, opening windows, or installing an update.
package updaterharness

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

@interface NSObject (RacomiUpdaterTest)
- (BOOL)updaterShouldRelaunchApplication:(id)updater;
@end

static int racomiUpdaterDelegateContract(void) {
 @autoreleasepool {
  Class delegateClass = NSClassFromString(@"RacomiUpdaterDelegate");
  if (!delegateClass) return 0;
  id delegate = [delegateClass new];
  int result = 1;
  if ([delegate respondsToSelector:@selector(updater:shouldPostponeRelaunchForUpdate:untilInvokingBlock:)]) result |= 2;
  if ([delegate updaterShouldRelaunchApplication:nil]) result |= 4;
  [delegate release];
  return result;
 }
}
*/
import "C"

func DelegateContract() (exists, postpones, relaunches bool) {
	flags := int(C.racomiUpdaterDelegateContract())
	return flags&1 != 0, flags&2 != 0, flags&4 != 0
}
