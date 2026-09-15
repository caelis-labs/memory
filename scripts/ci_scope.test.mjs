import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, renameSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { classifyPaths, inspectChanges } from './ci_scope.mjs';

function fixture(t) {
  const cwd = mkdtempSync(join(tmpdir(), 'memory-ci-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', ['-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null', ...args], {
    cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
  const write = (path, content) => {
    mkdirSync(dirname(join(cwd, path)), { recursive: true });
    writeFileSync(join(cwd, path), content);
  };
  const commit = () => { git('add', '-A'); git('commit', '--allow-empty', '-m', 'fixture'); return git('rev-parse', 'HEAD'); };
  git('init', '-b', 'main'); git('config', 'user.name', 'CI Test'); git('config', 'user.email', 'ci@example.invalid');
  write('.release-please-manifest.json', '{".":"0.5.2"}\n');
  write('VERSION', '0.6.0\n'); // The already-prepared first candidate.
  write('source.go', 'package example\n');
  const base = commit();
  const release = () => {
    write('.release-please-manifest.json', '{".":"0.6.0"}\n');
    write('CHANGELOG.md', '# Changelog\n\n## [0.6.0](https://example.invalid) (2026-09-15)\n');
  };
  return { cwd, git, write, commit, base, release };
}

test('only nonempty release-only diffs skip full checks', () => {
  assert.equal(classifyPaths([]).full, true);
  assert.equal(classifyPaths(['VERSION', 'CHANGELOG.md', '.release-please-manifest.json']).full, false);
  for (const path of ['README.md', 'SECURITY.md', 'docs/example.md', 'go.mod', 'go.sum', 'Makefile',
    '.github/workflows/quality.yml', 'scripts/ci_scope.mjs', 'internal/testdata/input.json', 'source.go',
    'VERSION\nsource.go', 'release-please-config.json', 'unknown']) {
    assert.equal(classifyPaths(['CHANGELOG.md', path]).full, true, path);
  }
});

test('first real release diff validates prepared VERSION and new changelog', t => {
  const f = fixture(t); f.release(); f.commit();
  assert.deepEqual(inspectChanges(f.base, f.cwd), {full: false});
});

test('subsequent release increments both version files', t => {
  const f = fixture(t); f.release(); const base = f.commit();
  f.write('.release-please-manifest.json', '{".":"0.6.1"}\n'); f.write('VERSION', '0.6.1\n');
  f.write('CHANGELOG.md', '# Changelog\n\n## 0.6.1\n'); f.commit();
  assert.equal(inspectChanges(base, f.cwd).full, false);
});

for (const [name, mutate, error] of [
  ['invalid JSON', f => f.write('.release-please-manifest.json', '{broken'), /JSON/],
  ['extra component', f => f.write('.release-please-manifest.json', '{".":"0.6.0","other":"0.6.0"}'), /one root/],
  ['prerelease', f => f.write('.release-please-manifest.json', '{".":"0.6.0-rc.1"}'), /stable version/],
  ['same version', f => f.write('.release-please-manifest.json', '{ ".": "0.5.2" }'), /must increase/],
  ['downgrade', f => f.write('.release-please-manifest.json', '{".":"0.5.1"}'), /must increase/],
  ['VERSION mismatch', f => f.write('VERSION', '0.7.0\n'), /VERSION must match/],
  ['VERSION whitespace', f => f.write('VERSION', ' 0.6.0\n\n'), /VERSION must match/],
  ['changelog mismatch', f => f.write('CHANGELOG.md', '# Changelog\n\n## 0.7.0\n'), /heading must match/],
  ['missing VERSION', f => rmSync(join(f.cwd, 'VERSION')), /regular non-executable/],
  ['missing changelog', f => rmSync(join(f.cwd, 'CHANGELOG.md')), /regular non-executable/],
  ['missing manifest', f => rmSync(join(f.cwd, '.release-please-manifest.json')), /regular non-executable/],
  ['executable metadata', f => chmodSync(join(f.cwd, 'CHANGELOG.md'), 0o755), /regular non-executable/],
  ['symlink metadata', f => { rmSync(join(f.cwd, 'VERSION')); symlinkSync('source.go', join(f.cwd, 'VERSION')); }, /regular non-executable/],
]) {
  test(`reject ${name}`, t => {
    const f = fixture(t); f.release(); mutate(f); f.commit();
    assert.throws(() => inspectChanges(f.base, f.cwd), error);
  });
}

test('renaming source into a metadata path still requires full checks', t => {
  const f = fixture(t); renameSync(join(f.cwd, 'source.go'), join(f.cwd, 'CHANGELOG.md')); f.commit();
  assert.equal(inspectChanges(f.base, f.cwd).full, true);
});

test('unrelated base and malformed base fail closed', t => {
  const f = fixture(t); f.git('checkout', '--orphan', 'unrelated');
  f.write('source.go', 'package unrelated\n'); f.commit();
  assert.throws(() => inspectChanges(f.base, f.cwd));
  assert.throws(() => inspectChanges('HEAD; unsafe', f.cwd), /full PR base SHA/);
});

test('bootstrap VERSION cannot be downgraded while manifest is unchanged', t => {
  const f = fixture(t); f.write('VERSION', '0.5.2\n');
  f.write('CHANGELOG.md', '# Changelog\n\n## 0.5.2\n'); f.commit();
  assert.throws(() => inspectChanges(f.base, f.cwd), /VERSION must not decrease/);
});
