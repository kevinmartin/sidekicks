# POC preservation

The repository began with an unborn main and no tracked files or remote. The first curated commit preserves the existing POC source, documentation, fixtures and tests. `reference/poc` and the annotated `poc-baseline-2026-10-30` tag identify that baseline before compute development.

The source baseline deliberately excludes generated evidence, logs, node_modules, caches, Python bytecode, transfer scratch files, VM build outputs/disks/state, archives and executable build outputs. They remain in place. Local inventory and hashes are in `artifacts/bootstrap/`; that directory is intentionally untracked. Existing Docker images and containers remain intact. No blanket git add, Docker prune, guest replacement or live-session restart was performed.

A Git branch is source preservation, not a disk/image backup. Ignored evidence and runtime assets still require a separately managed backup. Running guest disks were inventoried, not copied or claimed crash-consistent. No off-host backup or publication was performed. The exact local transport/image prerequisites described in the POC recipes remain necessary to reproduce historical images; mutable package repositories do not recreate old builds byte-for-byte.
