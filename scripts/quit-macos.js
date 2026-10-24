// Invoked by osascript: address only the exact bundle ID AND canonical path.
ObjC.import('AppKit');
ObjC.import('Foundation');
ObjC.bindFunction('kill', ['int', ['int', 'int']]);
function run(args) {
 const [bundleID, bundlePath] = args;
 const expected = ObjC.unwrap($(bundlePath).stringByStandardizingPath.stringByResolvingSymlinksInPath);
 const apps = $.NSRunningApplication.runningApplicationsWithBundleIdentifier($(bundleID));
 const targets = [];
 for (let i=0;i<apps.count;i++) {
  const app = apps.objectAtIndex(i);
  const path = ObjC.unwrap(app.bundleURL.path.stringByStandardizingPath.stringByResolvingSymlinksInPath);
  if (path === expected) targets.push(app);
 }
 for (const app of targets) if (!app.terminate) throw Error('The target application declined normal termination.');
 const pids = targets.map(app => Number(app.processIdentifier));
 const deadline = Date.now()+60000;
 // NSRunningApplication state is cached until its run loop advances. Signal 0
 // observes process lifetime without sending a signal or relying on that cache.
 while (pids.some(pid => $.kill(pid, 0) === 0)) {
  if (Date.now() > deadline) throw Error('The application is still running. Save changes and quit it before retrying.');
  $.NSThread.sleepForTimeInterval(0.1);
 }
 return 'Target application is stopped.';
}
