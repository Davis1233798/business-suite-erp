const path = require('node:path');

const ERP_DATA_DIRECTORY = 'ForgeFlow ERP';
const ERP_DATABASE_FILE = 'erp-repository.sqlite';

function desktopPaths(appDataDirectory) {
  if (typeof appDataDirectory !== 'string' || !path.isAbsolute(appDataDirectory)) {
    throw new Error('Electron appData path must be absolute.');
  }
  const userData = path.join(appDataDirectory, ERP_DATA_DIRECTORY);
  return Object.freeze({
    userData,
    database: path.join(userData, ERP_DATABASE_FILE),
  });
}

module.exports = { desktopPaths };
