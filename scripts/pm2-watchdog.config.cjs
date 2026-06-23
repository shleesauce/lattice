// PM2 ecosystem for the Lattice fleet watchdog on the hub host.
// Start:  pm2 start scripts/pm2-watchdog.config.cjs   (run from repo root)
// Logs:   pm2 logs lattice-watchdog
// Keeps the fleet "always on": detects a down agent, recovers it over SSH via
// that machine's own persistence primitive, and pushes to ntfy if it can't.
const fs = require('fs');
const path = require('path');
const root = path.resolve(__dirname, '..');
const token = fs.readFileSync(path.join(root, '.lattice-token'), 'utf8').trim();

module.exports = {
  apps: [{
    name: 'lattice-watchdog',
    script: path.join(root, 'scripts', 'fleet-watchdog.sh'),
    interpreter: 'bash',
    // Token via env (no --token argv on the watchdog). The script still reads
    // .lattice-token directly; this keeps token delivery consistent with the hub.
    // NOTE: pm2 persists env in ~/.pm2/dump.pm2, but it's out of `ps`/argv.
    env: { LATTICE_TOKEN: token },
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
