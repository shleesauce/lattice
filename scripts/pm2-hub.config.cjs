// PM2 ecosystem for the Lattice hub on the hub host (the always-on anchor).
// Start:   pm2 start scripts/pm2-hub.config.cjs   (run from repo root)
// Reload:  pm2 reload lattice-hub
// PM2 is the canonical process manager on the hub host.
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const token = fs.readFileSync(path.join(root, '.lattice-token'), 'utf8').trim();

module.exports = {
  apps: [{
    name: 'lattice-hub',
    script: path.join(root, 'dist', 'lattice-darwin-arm64'),
    // Token via env (the hub reads LATTICE_TOKEN) instead of --token argv, so the
    // master token is no longer visible in `ps`/`pm2 describe`. NOTE: pm2 still
    // persists env in ~/.pm2/dump.pm2 - it's just out of the process command line.
    args: ['hub', '--addr', ':7400', '--db', path.join(root, 'lattice.db')],
    env: { LATTICE_TOKEN: token },
    cwd: root,
    autorestart: true,
    max_restarts: 20,
    out_file: path.join(root, 'hub.out.log'),
    error_file: path.join(root, 'hub.err.log'),
    time: true,
  }],
};
