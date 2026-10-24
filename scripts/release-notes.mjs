#!/usr/bin/env node
import fs from 'node:fs';
import { execFileSync } from 'node:child_process';
import { parse, releaseNotes } from './release.mjs';

const [repo, tag] = process.argv.slice(2);
if (!repo || !tag || !/^[\w.-]+\/[\w.-]+$/.test(repo)) throw Error('Expected owner/repo and an existing release tag');
const config = JSON.parse(fs.readFileSync('.release.json', 'utf8'));
const prefix = config.tag_prefix ?? 'v';
if (!tag.startsWith(prefix)) throw Error('Unexpected tag prefix');
const version = tag.slice(prefix.length);
parse(version);
const commit = execFileSync('git', ['rev-parse', '--verify', `refs/tags/${tag}^{commit}`], {encoding:'utf8'}).trim();
process.stdout.write(releaseNotes(repo, prefix, version, commit, tag));
