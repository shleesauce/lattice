// PM2 ecosystem for the Lattice fleet watchdog on mini-ops.
// Start:  pm2 start scripts/pm2-watchdog.config.cjs   (run from repo root)
// Logs:   pm2 logs lattice-watchdog
// Keeps the fleet "always on": detects a down agent, recovers it over SSH via
// that machine's own persistence primitive, and pushes to ntfy if it can't.
const path = require('path');
const root = path.resolve(__dirname, '..');

module.exports = {
  apps: [{
    name: 'lattice-watchdog',
    script: path.join(root, 'scripts', 'fleet-watchdog.sh'),
    interpreter: 'bash',
    cwd: root,
    autorestart: true,
    max_restarts: 50,
    // Long-running loop; if it ever exits, wait a beat before pm2 relaunches.
    restart_delay: 5000,
    out_file: path.join(root, 'watchdog.out.log'),
    error_file: path.join(root, 'watchdog.err.log'),
    time: true,
  }],
};
