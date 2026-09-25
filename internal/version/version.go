// Package version is the desktop application's own version, reported to the browser
// extension over the loopback discovery endpoint.
//
// build/config.yml `info.version` is the single source of truth; scripts/version.mjs
// keeps this constant in lockstep with it and `node scripts/version.mjs --check` runs
// as a quality gate stage, so a manual edit here that forgets the config (or the other
// way round) fails the gate instead of shipping a mismatched desktop build.
package version

// Version is the released desktop version in X.Y.Z form.
const Version = "1.0.1"
