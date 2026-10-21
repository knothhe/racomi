import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { nextVersion, parse } from './release.mjs';

const releaseScript = fileURLToPath(new URL('./release.mjs', import.meta.url));
const projectMakefile = fileURLToPath(new URL('../Makefile', import.meta.url));
const jsonText = value => JSON.stringify(value, null, 2) + '\n';
const nodeCheck = source => [process.execPath, '--input-type=commonjs', '-e', source];
const checkMarker = nodeCheck("require('node:fs').writeFileSync('.git/check-ran', 'yes')");
const shellQuote = value => process.platform === 'win32' ? '"' + value.replaceAll('"', '\\"') + '"' : "'" + value.replaceAll("'", "'\\''") + "'";

function fixture(t, { versionFile = 'frontend/package.json', lockVersion = 3, shrinkwrap = false, config = {} } = {}) {
  const base = fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-release-test-'));
  t.after(() => fs.rmSync(base, { recursive: true, force: true }));
  const repo = path.join(base, 'work');
  const remote = path.join(base, 'remote.git');
  const emptyGitConfig = path.join(base, 'empty.gitconfig');
  fs.writeFileSync(emptyGitConfig, '');
  fs.mkdirSync(repo);
  // Ignore the user's Git configuration and environment; every push is to this
  // fixture's local bare repository, and only fixture commits/tags are created.
  const env = Object.fromEntries(Object.entries(process.env).filter(([name]) => !name.startsWith('GIT_')));
  Object.assign(env, {
    GIT_CONFIG_GLOBAL: emptyGitConfig, GIT_CONFIG_NOSYSTEM: '1',
    GIT_AUTHOR_NAME: 'Release test', GIT_AUTHOR_EMAIL: 'release-test@example.invalid',
    GIT_COMMITTER_NAME: 'Release test', GIT_COMMITTER_EMAIL: 'release-test@example.invalid',
  });
  const run = (command, args, ok = true) => {
    const result = spawnSync(command, args, { cwd: repo, env, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 });
    assert.ifError(result.error);
    if (ok) assert.equal(result.status, 0, `${command} ${args.join(' ')}\n${result.stdout}\n${result.stderr}`);
    else assert.notEqual(result.status, 0, `${command} unexpectedly succeeded\n${result.stdout}`);
    return result;
  };
  const git = (...args) => run('git', args).stdout.trim();
  const write = (name, content) => {
    fs.mkdirSync(path.dirname(path.join(repo, name)), { recursive: true });
    fs.writeFileSync(path.join(repo, name), content);
  };
  const read = name => fs.readFileSync(path.join(repo, name), 'utf8');
  const commit = (...files) => {
    git('add', '--', ...files);
    git('commit', '-m', 'Fixture');
  };
  const release = (args, ok = true) => run(process.execPath, [releaseScript, ...args], ok);
  const lockFiles = versionFile === 'VERSION' ? [] : [
    path.posix.join(path.posix.dirname(versionFile), 'package-lock.json'),
    ...(shrinkwrap ? [path.posix.join(path.posix.dirname(versionFile), 'npm-shrinkwrap.json')] : []),
  ];
  const releaseConfig = {
    version_file: versionFile, branch: 'main', remote: 'origin', tag_prefix: 'v',
    checks: [], check_phase: 'before-bump', github_release: false, ...config,
  };
  if (versionFile === 'VERSION') write(versionFile, '1.2.3\n');
  else write(versionFile, jsonText({ name: 'racomi-frontend', private: true, version: '1.2.3', dependencies: { demo: '^4.5.6' } }));
  for (const name of lockFiles) {
    write(name, jsonText({
      name: 'racomi-frontend', version: '1.2.3', lockfileVersion: lockVersion,
      ...(lockVersion >= 2 ? { packages: {
        '': { name: 'racomi-frontend', version: '1.2.3', dependencies: { demo: '^4.5.6' } },
        'node_modules/demo': { version: '4.5.6', integrity: 'fixture-integrity' },
      } } : { dependencies: { demo: { version: '4.5.6', integrity: 'fixture-integrity' } } }),
    }));
  }
  write('.release.json', jsonText(releaseConfig));
  run('git', ['init', '--bare', remote]);
  git('init', '-b', 'main');
  git('config', 'commit.gpgsign', 'false');
  git('config', 'tag.gpgsign', 'false');
  commit('.release.json', versionFile, ...lockFiles);
  git('remote', 'add', 'origin', remote);
  git('push', '-u', 'origin', 'main');
  const initial = git('rev-parse', 'HEAD');
  const snapshot = () => new Map([versionFile, ...lockFiles].map(name => [name, read(name)]));
  const assertSnapshot = expected => {
    for (const [name, content] of expected) assert.equal(read(name), content, name);
  };
  const assertUnreleased = () => {
    assert.equal(git('tag', '--list'), '');
    assert.equal(git('--git-dir', remote, 'rev-parse', 'main'), initial);
  };
  const configure = changes => {
    Object.assign(releaseConfig, changes);
    write('.release.json', jsonText(releaseConfig));
    commit('.release.json');
  };
  return { base, repo, remote, run, git, write, read, commit, release, configure, versionFile, lockFiles, initial, snapshot, assertSnapshot, assertUnreleased };
}

test('SemVer bumps, prerelease precedence and invalid versions', () => {
  for (const [bump, expected] of [['major', '2.0.0'], ['minor', '1.3.0'], ['patch', '1.2.4']]) {
    assert.equal(nextVersion('1.2.3', bump), expected);
  }
  assert.equal(nextVersion('1.3.0-rc.1', '1.3.0'), '1.3.0');
  assert.equal(nextVersion('1.2.3', '1.2.4+build.5'), '1.2.4+build.5');
  for (const value of ['01.2.3', '1.2', 'v1.2.3', '1.2.3-01', '1.2.3-a..b', '1.2.3+', '1.2.3\n']) {
    assert.throws(() => parse(value));
  }
  assert.throws(() => nextVersion('1.2.3', '1.2.3+build'));
  assert.throws(() => nextVersion('1.2.3-rc.1', 'patch'));
});

test('nested preview preserves files, HEAD and refs without running checks', t => {
  const f = fixture(t, { config: { checks: [checkMarker] } });
  const before = f.snapshot();
  const plan = JSON.parse(f.release(['patch']).stdout);
  assert.equal(plan.mode, 'preview');
  assert.equal(plan.current, '1.2.3');
  assert.equal(plan.target, '1.2.4');
  assert.equal(plan.check_phase, 'before-bump');
  assert.deepEqual(plan.files, [f.versionFile, ...f.lockFiles]);
  f.assertSnapshot(before);
  assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
  assert.equal(f.git('status', '--porcelain'), '');
  assert.equal(fs.existsSync(path.join(f.repo, '.git/check-ran')), false);
  f.assertUnreleased();
});

for (const versionFile of ['package.json', 'frontend/package.json']) {
  for (const lockVersion of [1, 2, 3]) {
    test(`${versionFile} upgrades and atomically pushes matching npm v${lockVersion} locks`, t => {
      const f = fixture(t, { versionFile, lockVersion, shrinkwrap: true });
      const before = f.snapshot();
      f.release(['minor', '--apply']);
      for (const [name, content] of before) {
        const expected = JSON.parse(content);
        expected.version = '1.3.0';
        if (f.lockFiles.includes(name) && lockVersion >= 2) expected.packages[''].version = '1.3.0';
        assert.deepEqual(JSON.parse(f.read(name)), expected);
      }
      assert.equal(f.git('log', '-1', '--format=%s'), 'chore(release): v1.3.0');
      assert.deepEqual(f.git('diff-tree', '--no-commit-id', '--name-only', '-r', 'HEAD').split('\n').sort(), [versionFile, ...f.lockFiles].sort());
      assert.equal(f.git('cat-file', '-t', 'v1.3.0'), 'tag');
      const head = f.git('rev-parse', 'HEAD');
      assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'main'), head);
      assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'v1.3.0^{commit}'), head);
      assert.equal(f.git('status', '--porcelain'), '');
    });
  }
}

test('initial release keeps nested files byte-for-byte and tags the existing commit', t => {
  const check = nodeCheck("const fs = require('node:fs'); if (JSON.parse(fs.readFileSync('frontend/package.json')).version !== '1.2.3') process.exit(2); fs.writeFileSync('.git/check-ran', 'yes')");
  const f = fixture(t, { config: { checks: [check], check_phase: 'after-bump' } });
  const before = f.snapshot();
  const plan = JSON.parse(f.release(['initial']).stdout);
  assert.equal(plan.release_type, 'initial');
  assert.equal(plan.check_phase, 'current-version');
  assert.equal(plan.create_version_commit, false);
  assert.deepEqual(plan.files, []);
  f.release(['initial', '--apply']);
  f.assertSnapshot(before);
  assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
  assert.equal(f.git('rev-list', '--count', 'HEAD'), '1');
  assert.equal(f.git('cat-file', '-t', 'v1.2.3'), 'tag');
  assert.match(f.git('for-each-ref', '--format=%(contents)', 'refs/tags/v1.2.3'), /Release-Type: initial/);
  assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'v1.2.3^{commit}'), f.initial);
  assert.equal(fs.readFileSync(path.join(f.repo, '.git/check-ran'), 'utf8'), 'yes');
  const resume = JSON.parse(f.release(['1.2.3', '--resume']).stdout);
  assert.equal(resume.release_type, 'initial');
  assert.deepEqual(resume.checks, []);
  f.release(['1.2.3', '--resume', '--apply']);
  assert.equal(f.git('rev-list', '--count', 'HEAD'), '1');
});

test('failed checks stop before changing nested version files or creating a tag', t => {
  const f = fixture(t, { config: { checks: [nodeCheck('process.exit(3)')] } });
  const before = f.snapshot();
  const result = f.release(['patch', '--apply'], false);
  assert.match(result.stderr, /Check failed/);
  f.assertSnapshot(before);
  assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
  assert.equal(f.git('status', '--porcelain'), '');
  f.assertUnreleased();
});

test('a check side effect is preserved and stops the remaining checks', t => {
  const f = fixture(t, { config: { checks: [nodeCheck("require('node:fs').writeFileSync('unexpected', 'keep')"), checkMarker] } });
  const before = f.snapshot();
  const result = f.release(['patch', '--apply'], false);
  assert.match(result.stderr, /Checks changed the working tree/);
  assert.equal(f.read('unexpected'), 'keep');
  assert.equal(fs.existsSync(path.join(f.repo, '.git/check-ran')), false);
  f.assertSnapshot(before);
  assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
  f.assertUnreleased();
});

test('after-bump check failures leave synchronized nested version edits for inspection', t => {
  const f = fixture(t, { config: { check_phase: 'after-bump', checks: [nodeCheck('process.exit(3)')] } });
  const result = f.release(['patch', '--apply'], false);
  assert.match(result.stderr, /Version edits remain for inspection/);
  for (const name of [f.versionFile, ...f.lockFiles]) assert.equal(JSON.parse(f.read(name)).version, '1.2.4');
  assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
  assert.equal(f.git('diff', '--cached', '--name-only'), '');
  f.assertUnreleased();
});

for (const collision of ['local', 'remote']) {
  test(`${collision} tag collisions are rejected before checks or version writes`, t => {
    const f = fixture(t, { config: { checks: [checkMarker] } });
    const before = f.snapshot();
    f.git('tag', '-a', 'v1.2.4', '-m', 'Already published');
    if (collision === 'remote') {
      f.git('push', 'origin', 'refs/tags/v1.2.4');
      f.git('tag', '-d', 'v1.2.4');
    }
    const result = f.release(['patch', '--apply'], false);
    assert.match(result.stderr, /[Tt]ag already exists/);
    f.assertSnapshot(before);
    assert.equal(fs.existsSync(path.join(f.repo, '.git/check-ran')), false);
    assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
    assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'main'), f.initial);
  });
}

test('an exact-version resume pushes the existing release without rerunning checks or bumping', t => {
  const check = nodeCheck("const fs = require('node:fs'); const name = '.git/check-count'; fs.writeFileSync(name, String(fs.existsSync(name) ? Number(fs.readFileSync(name)) + 1 : 1))");
  const f = fixture(t, { config: { checks: [check] } });
  f.release(['patch', '--apply', '--no-push']);
  const head = f.git('rev-parse', 'HEAD');
  assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'main'), f.initial);
  const plan = JSON.parse(f.release(['1.2.4', '--resume']).stdout);
  assert.deepEqual(plan.checks, []);
  assert.equal(plan.check_phase, null);
  f.release(['1.2.4', '--resume', '--apply']);
  assert.equal(f.git('rev-parse', 'HEAD'), head);
  assert.equal(f.git('rev-list', '--count', 'HEAD'), '2');
  assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'main'), head);
  assert.equal(f.git('--git-dir', f.remote, 'rev-parse', 'v1.2.4^{commit}'), head);
  assert.equal(fs.readFileSync(path.join(f.repo, '.git/check-count'), 'utf8'), '1');
});

test('nested lock version mismatches are rejected without changing files', t => {
  const f = fixture(t, { config: { checks: [checkMarker] } });
  const lock = JSON.parse(f.read(f.lockFiles[0]));
  lock.packages[''].version = '9.9.9';
  f.write(f.lockFiles[0], jsonText(lock));
  f.commit(f.lockFiles[0]);
  const before = f.snapshot();
  const result = f.release(['patch', '--apply'], false);
  assert.match(result.stderr, /root package version differs/);
  f.assertSnapshot(before);
  assert.equal(fs.existsSync(path.join(f.repo, '.git/check-ran')), false);
  f.assertUnreleased();
});

for (const versionFile of ['../package.json', '/tmp/package.json', './frontend/package.json', 'frontend/../package.json', 'frontend/other.json']) {
  test(`rejects unsafe or unsupported version path ${versionFile}`, t => {
    const f = fixture(t);
    f.configure({ version_file: versionFile });
    const result = f.release(['patch', '--apply'], false);
    assert.match(result.stderr, /repository-relative package.json path/);
    f.assertUnreleased();
  });
}

for (const symlink of ['manifest', 'directory', 'lock', 'dangling-lock']) {
  test(`rejects a symlinked ${symlink} before writing versions`, t => {
    const f = fixture(t);
    const createSymlink = (destination, name, type) => {
      try { fs.symlinkSync(destination, name, type); return true; }
      catch (error) {
        if (process.platform !== 'win32' || error.code !== 'EPERM') throw error;
        t.skip('symbolic links require Windows Developer Mode or elevation');
        return false;
      }
    };
    if (symlink === 'directory') {
      const destination = path.join(f.base, 'outside-frontend');
      f.git('rm', '--cached', '--', f.versionFile, ...f.lockFiles);
      fs.renameSync(path.join(f.repo, 'frontend'), destination);
      if (!createSymlink(destination, path.join(f.repo, 'frontend'), 'dir')) return;
      f.commit('frontend');
    } else {
      const name = symlink === 'manifest' ? f.versionFile : f.lockFiles[0];
      const destination = path.join(f.base, 'outside.json');
      if (symlink !== 'dangling-lock') fs.writeFileSync(destination, f.read(name));
      fs.unlinkSync(path.join(f.repo, name));
      if (!createSymlink(destination, path.join(f.repo, name), 'file')) return;
      f.commit(name);
    }
    const result = f.release(['patch', '--apply'], false);
    assert.match(result.stderr, /symlinked version path/);
    assert.equal(f.git('status', '--porcelain'), '');
    f.assertUnreleased();
  });
}

for (const workspace of ['package.json', 'frontend/package.json', 'pnpm-workspace.yaml', 'frontend/pnpm-workspace.yaml']) {
  test(`rejects workspace configuration in ${workspace}`, t => {
    const f = fixture(t);
    if (workspace.endsWith('.json')) {
      const manifest = fs.existsSync(path.join(f.repo, workspace)) ? JSON.parse(f.read(workspace)) : { private: true };
      manifest.workspaces = ['packages/*'];
      f.write(workspace, jsonText(manifest));
    } else f.write(workspace, 'packages:\n  - packages/*\n');
    f.commit(workspace);
    const result = f.release(['patch', '--apply'], false);
    assert.match(result.stderr, /Workspace\/monorepo detected/);
    assert.equal(f.git('status', '--porcelain'), '');
    f.assertUnreleased();
  });
}

test('root VERSION remains supported without a Node manifest', t => {
  const f = fixture(t, { versionFile: 'VERSION' });
  f.release(['patch', '--apply']);
  assert.equal(f.read('VERSION'), '1.2.4\n');
  assert.equal(f.git('cat-file', '-t', 'v1.2.4'), 'tag');
});

test('project Make release and release-initial targets use preview mode by default', t => {
  const f = fixture(t);
  // Keep the recipe's script path inside the temporary fixture. Native Windows
  // make can encode non-ASCII absolute recipe arguments using the ANSI codepage.
  const fixtureReleaseScript = path.join(f.base, 'release.mjs');
  fs.copyFileSync(releaseScript, fixtureReleaseScript);
  const releaseCommand = `RELEASE_COMMAND=${shellQuote(process.execPath)} ${shellQuote(fixtureReleaseScript)}`;
  for (const [target, releaseType] of [['release', 'upgrade'], ['release-initial', 'initial']]) {
    const plan = JSON.parse(f.run('make', ['--silent', '-I', path.dirname(projectMakefile), '-f', projectMakefile, target, 'VERSION_BUMP=minor', releaseCommand]).stdout);
    assert.equal(plan.mode, 'preview');
    assert.equal(plan.release_type, releaseType);
    assert.equal(plan.target, target === 'release' ? '1.3.0' : '1.2.3');
  }
  assert.equal(f.git('rev-parse', 'HEAD'), f.initial);
  f.assertUnreleased();
});
