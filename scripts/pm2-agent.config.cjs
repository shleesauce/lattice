// PM2 ecosystem for the Lattice agent on the hub host (mini-ops runs both).
// Start:   pm2 start scripts/pm2-agent.config.cjs   (run from repo root)
// Reload:  pm2 restart lattice-agent
// PM2 is the canonical process manager on the hub host.
//
// This file exists because the agent was originally started with an ad-hoc
// `pm2 start ... -- agent --token <CODE>`, which put the enrollment token in the
// process command line where `ps` and `pm2 describe` print it in the clear. The
// hub config already avoided that; this brings the agent to parity.
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const token = fs.readFileSync(path.join(root, '.lattice-token'), 'utf8').trim();

module.exports = {
  apps: [{
    name: 'lattice-agent',
    script: path.join(root, 'dist', 'lattice-darwin-arm64'),
    // Token via env (the agent falls back to LATTICE_TOKEN when --token is
    // absent - see internal/agent/agent.go parseFlags) instead of --token argv,
    // so it is no longer visible in `ps` / `pm2 describe`. NOTE: pm2 still
    // persists env in ~/.pm2/dump.pm2 - it's just out of the process command line.
    args: ['agent', '--hub', '127.0.0.1:7400', '--name', 'mini-ops'],
    env: { LATTICE_TOKEN: token },
    cwd: root,
    // autorestart is what makes the self-update path work: on `lattice update`
    // the agent swaps its binary, acks, and exits; PM2 relaunches it on the new
    // build with no manual step (see internal/agent/update.go, D40).
    autorestart: true,
    max_restarts: 20,
    out_file: path.join(root, 'agent.out.log'),
    error_file: path.join(root, 'agent.err.log'),
    time: true,
  }],
};
