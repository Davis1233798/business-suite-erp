const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const packageJson = require('../desktop/package.json');
const mainSource = fs.readFileSync(path.join(__dirname, '../desktop/main.cjs'), 'utf8');
const { desktopPaths } = require('../desktop/paths.cjs');

test('ERP desktop package has a dedicated operating-system identity', () => {
  assert.equal(packageJson.build.appId, 'org.blockchen.forgeflow.erp');
  assert.equal(packageJson.build.productName, 'ForgeFlow ERP');
});

test('ERP desktop userData and database stay inside the ERP namespace', () => {
  const appData = path.resolve('fixture-app-data');
  const paths = desktopPaths(appData);

  assert.equal(paths.userData, path.join(appData, 'ForgeFlow ERP'));
  assert.equal(paths.database, path.join(appData, 'ForgeFlow ERP', 'erp-repository.sqlite'));
  assert.equal(path.dirname(paths.database), paths.userData);
  assert.notEqual(paths.database, path.join(appData, 'Business Suite', 'suite.sqlite'));
});

test('ERP desktop paths reject invalid appData roots', () => {
  assert.throws(() => desktopPaths('relative-app-data'), /must be absolute/);
  assert.throws(() => desktopPaths(null), /must be absolute/);
});

test('Electron applies the ERP userData before starting its backend', () => {
  const setUserData = mainSource.indexOf('app.setPath(\'userData\',dataPaths.userData)');
  const startBackend = mainSource.indexOf('app.whenReady()');

  assert.notEqual(setUserData, -1);
  assert.notEqual(startBackend, -1);
  assert.ok(setUserData < startBackend);
  assert.match(mainSource, /SUITE_DB:dataPaths\.database/);
  assert.doesNotMatch(mainSource, /suite\.sqlite/);
});
