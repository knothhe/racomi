package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreText
#import <Foundation/Foundation.h>
#import <CoreText/CoreText.h>
#include <stdlib.h>

static char *racomiInterfaceFonts(void) {
 @autoreleasepool {
  CTFontCollectionRef collection = CTFontCollectionCreateFromAvailableFonts(NULL);
  if (!collection) return NULL;
  CFArrayRef descriptors = CTFontCollectionCreateMatchingFontDescriptors(collection);
  NSMutableDictionary *families = [NSMutableDictionary dictionary];
  for (CFIndex i = 0; descriptors && i < CFArrayGetCount(descriptors); i++) {
   CTFontDescriptorRef descriptor = (CTFontDescriptorRef)CFArrayGetValueAtIndex(descriptors, i);
   NSString *family = (NSString *)CTFontDescriptorCopyAttribute(descriptor, kCTFontFamilyNameAttribute);
   NSString *style = (NSString *)CTFontDescriptorCopyAttribute(descriptor, kCTFontStyleNameAttribute);
   NSString *name = (NSString *)CTFontDescriptorCopyAttribute(descriptor, kCTFontNameAttribute);
   if (family.length && name.length && ![family hasPrefix:@"."]) {
    NSMutableArray *styles = families[family];
    if (!styles) { styles = [NSMutableArray array]; families[family] = styles; }
    [styles addObject:@{@"name": style.length ? style : @"Regular", @"postscriptName": name}];
   }
   [family release]; [style release]; [name release];
  }
  if (descriptors) CFRelease(descriptors);
  CFRelease(collection);
  NSMutableArray *result = [NSMutableArray array];
  for (NSString *family in [[families allKeys] sortedArrayUsingSelector:@selector(localizedCaseInsensitiveCompare:)]) {
   NSArray *styles = [families[family] sortedArrayUsingDescriptors:@[[NSSortDescriptor sortDescriptorWithKey:@"name" ascending:YES selector:@selector(localizedCaseInsensitiveCompare:)]]];
   [result addObject:@{@"family": family, @"styles": styles}];
  }
  NSData *data = [NSJSONSerialization dataWithJSONObject:result options:0 error:nil];
  return data ? strdup([[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] autorelease].UTF8String) : NULL;
 }
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

func listInterfaceFonts() ([]InterfaceFontFamily, error) {
	data := C.racomiInterfaceFonts()
	if data == nil {
		return nil, errors.New("could not read installed fonts")
	}
	defer C.free(unsafe.Pointer(data))
	fonts := []InterfaceFontFamily{}
	err := json.Unmarshal([]byte(C.GoString(data)), &fonts)
	return fonts, err
}
