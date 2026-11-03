const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

const workflow = fs.readFileSync('.github/workflows/erp.yml', 'utf8');
const erpJob = workflow.split('\n  erp:\n')[1].split('\n  release:\n')[0];
const releaseJob = workflow.split('\n  release:\n')[1];

test('ERP image publication waits for ERP and Windows desktop validation', () => {
  assert.match(releaseJob, /^    needs: \[erp, windows-desktop\]$/m);
  assert.doesNotMatch(releaseJob, /^    if: /m, 'release build must run on pull requests as well as main pushes');
  assert.doesNotMatch(erpJob, /docker push/);
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
  assert.match(publicationStep, /docker push "\$image:\$\{GITHUB_SHA\}"/);
});
