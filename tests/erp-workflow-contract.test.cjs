const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

const workflow = fs.readFileSync('.github/workflows/erp.yml', 'utf8');
const erpJob = workflow.split('\n  erp:\n')[1].split('\n  release:\n')[0];
const hostJob = workflow.split('\n  rootless-host:\n')[1].split('\n  release:\n')[0];
const releaseJob = workflow.split('\n  release:\n')[1].split('\n  deploy:\n')[0];
const deployJob = workflow.split('\n  deploy:\n')[1];

test('ERP image publication waits for ERP and Windows desktop validation', () => {
  assert.match(releaseJob, /^    needs: \[erp, windows-desktop, rootless-host\]$/m);
  assert.doesNotMatch(releaseJob, /^    if: /m, 'release build must run on pull requests as well as main pushes');
  assert.doesNotMatch(erpJob, /docker push/);
});

test('ERP rootless host acceptance is isolated to an ephemeral ARM runner', () => {
  assert.match(hostJob, /^    runs-on: ubuntu-24\.04-arm$/m);
  assert.match(hostJob, /ssh-keygen -q -t ed25519 -N '' -f "\$RUNNER_TEMP\/erp-fixture-key"/);
  assert.match(hostJob, /provision-erp-rootless-host\.sh --apply --deploy-public-key-file/);
  assert.match(hostJob, /test "\$\(systemctl is-active docker\.service\)" = active/);
  assert.doesNotMatch(hostJob, /secrets\.|ERP_DEPLOY_TARGET|gh api .*erp-production/);
});

test('release job creates Docker web assets before building the ERP image', () => {
  const install = releaseJob.indexOf('npm run install:ci');
  const webBuild = releaseJob.indexOf('npm run web:build');
  const dockerBuild = releaseJob.indexOf('docker build -f infra/linux/Dockerfile.station');

  assert.ok(install >= 0);
  assert.ok(webBuild > install);
  assert.ok(dockerBuild > webBuild);
});

test('only a main push publishes the ERP image', () => {
  const publicationStep = releaseJob.split('\n      - name: Publish ERP image after all platform checks\n')[1];

  assert.ok(publicationStep);
  assert.match(publicationStep, /^        if: github\.event_name == 'push' && github\.ref == 'refs\/heads\/main'$/m);
  assert.match(publicationStep, /tag="\$\{GITHUB_SHA\}\$\{IMAGE_TAG_SUFFIX\}"/);
  assert.match(publicationStep, /docker push "\$image:\$tag"/);
});

test('deployment uses the same main workflow and its pinned ARM64 artifact', () => {
  assert.match(deployJob, /^    needs: \[release, rootless-host\]$/m);
  assert.match(deployJob, /^    if: github\.event_name == 'push' && github\.ref == 'refs\/heads\/main' && vars\.ERP_DEPLOY_ENABLED == 'true'$/m);
  assert.match(deployJob, /erp-arm64-release-\$\{\{ github\.sha \}\}/);
  assert.match(deployJob, /StrictHostKeyChecking=yes/);
});
