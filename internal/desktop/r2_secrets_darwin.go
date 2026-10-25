//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework Security
#import <Foundation/Foundation.h>
#import <Security/Security.h>
#include <stdlib.h>
#include <string.h>

static NSMutableDictionary *racomiR2SecretQuery(const char *service, const char *key) {
 return [NSMutableDictionary dictionaryWithDictionary:@{
  (id)kSecClass: (id)kSecClassGenericPassword,
  (id)kSecAttrService: [NSString stringWithUTF8String:service],
  (id)kSecAttrAccount: [NSString stringWithUTF8String:key]
 }];
}

static int racomiR2SecretLoad(const char *service, const char *key, void **bytes, size_t *size) {
 @autoreleasepool {
  NSMutableDictionary *query = racomiR2SecretQuery(service, key);
  query[(id)kSecReturnData] = @YES;
  query[(id)kSecMatchLimit] = (id)kSecMatchLimitOne;
  CFTypeRef result = NULL;
  OSStatus status = SecItemCopyMatching((CFDictionaryRef)query, &result);
  if (status != errSecSuccess) return (int)status;
  if (!result || CFGetTypeID(result) != CFDataGetTypeID()) {
   if (result) CFRelease(result);
   return (int)errSecDecode;
  }
  CFDataRef data = (CFDataRef)result;
  CFIndex length = CFDataGetLength(data);
  if (length <= 0 || length > 65536) { CFRelease(result); return (int)errSecDecode; }
  *bytes = malloc((size_t)length);
  if (!*bytes) { CFRelease(result); return (int)errSecAllocate; }
  *size = (size_t)length;
  memcpy(*bytes, CFDataGetBytePtr(data), (size_t)length);
  CFRelease(result);
  return (int)errSecSuccess;
 }
}

static int racomiR2SecretSave(const char *service, const char *key, const void *bytes, size_t size) {
 @autoreleasepool {
  NSMutableDictionary *query = racomiR2SecretQuery(service, key);
  NSData *data = [NSData dataWithBytes:bytes length:size];
  OSStatus status = SecItemUpdate((CFDictionaryRef)query, (CFDictionaryRef)@{(id)kSecValueData: data});
  if (status == errSecItemNotFound) {
   query[(id)kSecValueData] = data;
   status = SecItemAdd((CFDictionaryRef)query, NULL);
   // Another instance may have added this exact item between update and add.
   if (status == errSecDuplicateItem) {
    [query removeObjectForKey:(id)kSecValueData];
    status = SecItemUpdate((CFDictionaryRef)query, (CFDictionaryRef)@{(id)kSecValueData: data});
   }
  }
  return (int)status;
 }
}

static int racomiR2SecretDelete(const char *service, const char *key) {
 @autoreleasepool { return (int)SecItemDelete((CFDictionaryRef)racomiR2SecretQuery(service, key)); }
}

static void racomiR2SecretFree(void *bytes, size_t size) {
 if (bytes) { memset_s(bytes, size, 0, size); free(bytes); }
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type r2NativeSecretBackend struct{}

func r2KeychainError(operation string, status C.int) error {
	if status == C.errSecItemNotFound {
		return errR2SecretNotFound
	}
	if status == C.errSecUserCanceled || status == C.errSecAuthFailed || status == C.errSecInteractionNotAllowed {
		return errR2SecretAccessDenied
	}
	return fmt.Errorf("macOS Keychain could not %s Cloudflare credentials (status %d)", operation, int(status))
}

func (r2NativeSecretBackend) load(service, key string) ([]byte, error) {
	serviceC, keyC := C.CString(service), C.CString(key)
	defer C.free(unsafe.Pointer(serviceC))
	defer C.free(unsafe.Pointer(keyC))
	var bytes unsafe.Pointer
	var size C.size_t
	status := C.racomiR2SecretLoad(serviceC, keyC, &bytes, &size)
	if status != C.errSecSuccess {
		return nil, r2KeychainError("read", status)
	}
	defer C.racomiR2SecretFree(bytes, size)
	return C.GoBytes(bytes, C.int(size)), nil
}

func (r2NativeSecretBackend) save(service, key string, value []byte) error {
	serviceC, keyC := C.CString(service), C.CString(key)
	defer C.free(unsafe.Pointer(serviceC))
	defer C.free(unsafe.Pointer(keyC))
	status := C.racomiR2SecretSave(serviceC, keyC, unsafe.Pointer(&value[0]), C.size_t(len(value)))
	if status != C.errSecSuccess {
		return r2KeychainError("save", status)
	}
	return nil
}

func (r2NativeSecretBackend) delete(service, key string) error {
	serviceC, keyC := C.CString(service), C.CString(key)
	defer C.free(unsafe.Pointer(serviceC))
	defer C.free(unsafe.Pointer(keyC))
	status := C.racomiR2SecretDelete(serviceC, keyC)
	if status != C.errSecSuccess {
		return r2KeychainError("delete", status)
	}
	return nil
}
