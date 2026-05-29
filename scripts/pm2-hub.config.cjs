// PM2 ecosystem for the Lattice hub on mini-ops (the always-on anchor).
// Start:   pm2 start scripts/pm2-hub.config.cjs   (run from repo root)
// Reload:  pm2 reload lattice-hub
// PM2 is the canonical process manager on mini-ops (matches the homebase pattern).
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const token = fs.readFileSync(path.join(root, '.lattice-token'), 'utf8').trim();

module.exports = {
  apps: [{
    name: 'lattice-hub',
    script: path.join(root, 'dist', 'lattice-darwin-arm64'),
    args: ['hub', '--addr', ':7400', '--db', path.join(root, 'lattice.db'), '--token', token],
    cwd: root,
    autorestart: true,
    max_restarts: 20,
    out_file: path.join(root, 'hub.out.log'),
    error_file: path.join(root, 'hub.err.log'),
    time: true,
  }],
};
