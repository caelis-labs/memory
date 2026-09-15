import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const releaseFiles = new Set(['.release-please-manifest.json', 'CHANGELOG.md', 'VERSION']);
const stableVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

// Only a nonempty diff consisting entirely of release metadata avoids full
// checks. Unknown paths, docs, build inputs and this classifier fail closed.
export function classifyPaths(paths) {
  return { full: paths.length === 0 || paths.some(path => !releaseFiles.has(path)) };
}

export function manifestVersion(content) {
  const value = JSON.parse(content);
  if (!value || Array.isArray(value) || Object.keys(value).length !== 1 ||
      typeof value['.'] !== 'string' || !stableVersion.test(value['.'])) {
    throw new Error('release manifest must contain one root stable version');
  }
  return value['.'];
}

function compareVersions(after, before) {
  const left = after.split('.').map(BigInt);
  const right = before.split('.').map(BigInt);
  const index = left.findIndex((part, index) => part !== right[index]);
  return index < 0 ? 0 : left[index] > right[index] ? 1 : -1;
}

export function inspectChanges(base, cwd = process.cwd()) {
  if (!/^[a-f0-9]{40}$/.test(base ?? '')) throw new Error('a full PR base SHA is required');
  const git = (...args) => execFileSync('git', args, {cwd, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024});
  git('merge-base', '--is-ancestor', base, 'HEAD');
  const paths = git('diff', '--no-ext-diff', '--no-textconv', '--no-renames', '--name-only', '-z', base, 'HEAD', '--')
    .split('\0').filter(Boolean);
  const scope = classifyPaths(paths);
  if (scope.full) return scope;
  for (const path of releaseFiles) {
    if (!git('ls-tree', 'HEAD', '--', path).startsWith('100644 blob ')) {
      throw new Error(`${path} must be a regular non-executable file`);
    }
  }
  const version = manifestVersion(git('show', 'HEAD:.release-please-manifest.json'));
  if (paths.includes('.release-please-manifest.json')) {
    const previous = manifestVersion(git('show', `${base}:.release-please-manifest.json`));
    if (compareVersions(version, previous) <= 0) throw new Error(`release version must increase from ${previous}, got ${version}`);
  }
  if (git('show', 'HEAD:VERSION') !== `${version}\n`) throw new Error(`VERSION must match ${version} with one trailing newline`);
  const previousVersion = git('show', `${base}:VERSION`).trim();
  if (!stableVersion.test(previousVersion) || compareVersions(version, previousVersion) < 0) {
    throw new Error(`VERSION must not decrease from ${previousVersion}`);
  }
  const heading = git('show', 'HEAD:CHANGELOG.md').split(/\r?\n/).find(line => line.startsWith('## '));
  const match = heading?.match(/^## (?:\[(\d+\.\d+\.\d+)\]|(\d+\.\d+\.\d+)(?=\s|$))/);
  if ((match?.[1] ?? match?.[2]) !== version) throw new Error(`first changelog release heading must match ${version}`);
  console.log(`Release metadata validated: ${version}`);
  return scope;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const scope = inspectChanges(process.env.PR_BASE_SHA);
    console.log(`Required checks: ${JSON.stringify(scope)}`);
    appendFileSync(process.env.GITHUB_OUTPUT, `full=${scope.full}\n`);
  } catch (error) {
    console.error(`CI change inspection failed: ${error.message}`);
    process.exitCode = 1;
  }
}
