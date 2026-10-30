# Xfce desktop candidate integration

Run `prepare.sh EXACT_NOVNC_IMAGE_ID EXACT_NEKO_IMAGE_ID` only after both Docker stages are frozen and actual desktop checks pass. It makes separate candidate filesystems under `.build/desktop-polish-v1/`, retaining current canonical templates, runners and live guests. The Neko copy retains the previously verified loopback-candidate binary and private vsock/network behavior. No host mount, network-enabled extraction helper, privileged container, new kernel or signing entitlement is needed.

Preserved v1 includes Neko v6, which failed independent Firefox Home acceptance. Promotion explicitly rejects that image before stopping any guest. The corrected v7 was independently verified, prepared under v2 and promoted to the three canonical VMs. Bounded production interaction, one normal restart per VM, logout Cancel, actual held-input cleanup and each existing LAN viewer passed; the final all-five passive audit also passed. See `artifacts/desktop-polish/production/final-readiness.json`, `artifacts/independent-qa/desktop-polish/production-summary.json` and the transition journal.

`SIDEKICKS_POLISH_REVISION=v2` selects a separate successor directory. During preparation, `SIDEKICKS_POLISH_REUSE_NOVNC_FROM=v1` verifies its exact source image, size and SHA before APFS-cloning only the unchanged noVNC template; Neko is freshly exported/formatted. `stage.py` and `promote.py` use the same validated revision selector. Old v1 templates/manifests remain untouched. Select v2 explicitly for successor staging/promotion; do not repoint a current guest disk or overwrite a failed candidate to hide its history.

Preparation checks20GiB available first for sequential extraction (including a4GiB reserve), records exact IDs, verifies ext4 and expected image length, and removes only new reproducible export tars after success. The formatter is the preserved0bdd image, so the clean final runtime need not retain filesystem build tools. NAT uses an APFS clone of the same Neko template; boot parameters supply its networking differences. It refuses to overwrite completed candidates or replace uncertain existing helpers. Previous desktop state/evidence stays intact.

An isolated noVNC guest can boot on separate endpoints using these templates without disturbing canonical `a`, `neko` or `nat`. Do not start a parallel NAT clone while canonical `nat` is active: an actual candidate reported the same DHCP address, and simultaneous clone identity remains unvalidated. The relay's fixed host UDP allocation is already occupied: do not start a duplicate relay against those ports. Final deployment/QA requires a controlled handoff and actual session inventory.

Kevin has explicitly finished the Docker7901 comparison and authorized its restart. Promotion still waits for staged checks and an exclusive handoff; preserve the old container/image and VM disks/templates for rollback. Stage restart evidence remains distinct from production restart evidence. See `docs/architecture/desktop-stack.md` and the independent desktop-polish protocol.

The copied PID1 agent starts `/app/entrypoint-xfce.sh` when present and recreates the root-owned sticky `/tmp/.ICE-unix` after mounting tmpfs. Neko's final supervisor exports its session D-Bus/XDG/locale environment to every child; image ENV is not assumed to survive `docker export`. Existing kernels, signed runners, private RPC, source-address separation, loopback endpoints and UDP/NAT mechanisms stay unchanged.

Prepared candidates can be inspected and booted separately:

```bash
SIDEKICKS_POLISH_REVISION=v2 python3 pocs/microvm/polish/stage.py novnc # Read-only plan
# Creation requires a fresh stage name/state and an exclusive handoff.
SIDEKICKS_POLISH_REVISION=v2 python3 pocs/microvm/polish/stage.py novnc --start
# NAT staging requires canonical nat to be stopped first; no parallel-NAT acceptance.
SIDEKICKS_POLISH_REVISION=v2 python3 pocs/microvm/polish/stage.py nat
python3 pocs/microvm/microvm.py status polish-a
python3 pocs/microvm/microvm.py status polish-nat
```

Only after the stage's exclusive QA window is released, stop/destroy just these owned stages with the existing session CLI. Never destroy `a`, `neko` or `nat` for rollout. `stage.py` refuses an existing stage instead of silently recreating an uncertain outcome.

Promotion is a separate explicit action after staged acceptance and an exclusive production handoff:

```bash
SIDEKICKS_POLISH_REVISION=v2 python3 pocs/microvm/polish/promote.py --verify-hashes
# Already applied; the journal/backup guard refuses a blind repeat.
SIDEKICKS_POLISH_REVISION=v2 python3 pocs/microvm/polish/promote.py --apply
```

Promotion has already run once: its backup/journal guard deliberately refuses a blind repeat. Normal operation uses `microvm.py start/stop/status a|neko|nat`, not another promotion.

The journal is `artifacts/desktop-polish/promotion.json`. Prior canonical templates/runners/kernel are cloned to `.build/pre-desktop-polish-v1/`; stopped prior disks/manifests move to `.state/{a,neko,nat}-pre-polish-v1/`. New guests use the same signed runners/kernel and endpoints. A partial failure stays explicit; inspect the journal/processes before retry or rollback. The script does not restart the LAN bridge: compare its recorded NAT guest IP to the new `nat` info, refreshing only that task-owned bridge if the address changed.

Rollback is performed in an exclusive window: sync-stop any new canonical sessions; archive their state directories separately; APFS-clone the three saved `base*.ext4` templates back atomically; move each prior `*-pre-polish-v1` directory back to its canonical name; then `start a`, `start neko`, `start nat`. Check owners/actual held input and LAN NAT address before returning service. Do not remove either generation or run Docker/VM prune. No promotion or rollback is implied by merely preparing these scripts.

The read-only noVNC stage decoded correctly and is stopped. A parallel NAT candidate reported the same guest IP as canonical `nat`; it was immediately stopped without media/input acceptance, and the old canonical stream subsequently decoded successfully. During actual promotion the old NAT was stopped before the new one started, avoiding concurrent NAT clones. Current NAT/media and the LAN target must be checked after every coordinated restart; simultaneous NAT clone identity remains unvalidated. No host routing/firewall settings were changed.
