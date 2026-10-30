# Desktop polish rollout

All five production desktops now run the independently verified Xfce/GTK3 candidates. **Bounded interaction, one normal restart per target, existing LAN viewing and the final passive audit passed.** All QA windows are released. Production remains running; disposable Docker stages/proxy are stopped with containers/images preserved.

| Production target | Local viewer | Existing LAN viewer | Current acceptance |
|---|---|---|---|
| noVNC Docker | http://127.0.0.1:7901/ | http://192.168.0.97:17901/ | Eight production checks passed across preserved attempts, including supported stop/start, postrestart Cancel and LAN |
| Neko Docker | http://127.0.0.1:7902/ | http://192.168.0.97:17902/ | Ten production checks passed, including supported stop/start, both input routes, LAN and logout Cancel |
| noVNC VM A | http://127.0.0.1:7903/ | http://192.168.0.97:17903/ | Eight production checks passed across preserved attempts, including one normal restart, postrestart Cancel and LAN |
| Neko relay VM | http://127.0.0.1:7906/ | http://192.168.0.97:17906/ | Ten production checks passed, including one normal restart, native input, LAN and logout Cancel |
| Neko NAT VM | http://127.0.0.1:7907/ | http://192.168.0.97:17907/ | Ten production checks passed, including one normal restart, native input, LAN and logout Cancel |

The Mac address is 192.168.0.97 on en9. The existing LAN bridge and workbench are unchanged. Neko's native diagnostic route adds `?mode=native`. Physical phone testing remains deferred.

## Accepted candidates and deployment

| Candidate | Exact image | Docker bytes | Net from previous accepted image |
|---|---|---:|---:|
| noVNC | `sha256:f8dee4d59f9bb8b26676928b80f784cd4f982a591812c0f1fe501d0b564a1b38` | 1,018,096,080 | −41,146,373 |
| Neko v7 | `sha256:36e11c585ece469a01f5bf705d5e1357afe767ba8e858eafba548d38786457a1` | 1,558,730,439 | +91,181,958 |

Independent stage acceptance is recorded in `artifacts/independent-qa/desktop-polish/stage-report.md` (SHA256 `286b61c9f21869a322d65a4cc60d0e9ed0774e1f3619e6b32a27e5cf12196f5f`). NoVNC's final stage result contains eight PASS plus five referenced prior PASS. Neko's v7 has seven focused Home-policy PASS, with unaffected v6 evidence explicitly referenced; it is not a full v7 app resuite.

The Docker production containers are `98d236dbc3321d82417ec2de98ee39aa02c0beda15fff3c191cd59f2cdf433b8` (noVNC) and `127b2d00b590d3a32f3d1563503a1269558fcaa37df0bace04b26c6134454ae3` (Neko). Current postrestart identities and exact source/template provenance are recorded in `artifacts/desktop-polish/production/final-readiness.json`; the earlier `readiness.json` intentionally retains pre-QA boot IDs. The transition journal is `pocs/microvm/artifacts/desktop-polish/promotion.json`.

Production acceptance totals35 new PASS plus11 explicitly referenced PASS groups,46 total, with exactly five normal restarts. Integration rechecked all final result hashes and recursively resolved referenced checks to original passing evidence. Final audit at18:31UTC: all five healthy/ready, one Home browser each, sole session components, owner null and actual held input empty;11 private TCP ports loopback-only,5 authorized LAN TCP wildcard ports unchanged; workbench200 and historical hashes unchanged. Disposable stage cleanup preserved production identities/StartedAt and all service health remained200. Evidence: `artifacts/independent-qa/desktop-polish/{production-summary,final-production-state}.json` and `artifacts/desktop-polish/production/stage-cleanup.json`.

The frozen independent final report is `docs/desktop-polish-verification.md` (SHA256 `5eaf6dfa0bee61fde4cb55aee1c96343ae880333286bdf79a5f397246a82f439`), saved to a new Library identity/version recorded in `artifacts/desktop-polish/library-deliverables.json`. Earlier Library reports and local frozen reports remain unchanged; no new screenshots were uploaded.

After that audit, Kevin reported an accidental7901 Log Out. Actual Xfce/apps had already ended, so integration performed one additional operational recovery stop/start of the same container/image, retaining its filesystem/browser profile. Actual1280×720 RFB Home and the real Home-button input passed; final owner/held input were empty and other Docker identities were unchanged. `artifacts/desktop-polish/recovery-7901/recovery.json` contains the newer7901 StartedAt; the frozen QA audit/counts remain historical and unchanged.

Kevin released the original 7901 comparison and authorized normal restart/replacement. Old Docker containers/images and stopped VM states/templates remain available for rollback. Screenshots and files do not represent cloned live terminal jobs.

## Behavior and retained limitations

The current session uses one Xfwm4 compositor, xfsettingsd, xfdesktop, xfce4-panel, Thunar GTK3 and GTK3/VTE xfce4-terminal. Chromium uses embedded controls; Neko retains Firefox with normal controls. Shared managed helpers supply the dock, curated desktop menu, Home, shortcuts and default browser associations. Original Sidekicks styling inherits Adwaita; no proprietary OrbitOS bundle was copied.

Supported named-user panel locking removes customization access. Help/About/Log Out remain. Help opens an online-manual prompt; external loading in no-NIC guests is untested. `PromptOnLogout=true` provides a real confirmation; actual Cancel preserves app/session identities and content. Confirming logout intentionally ends applications. Neko recreates its session; noVNC reports 503/ready:false until restarted. Closing only the browser retains desktop readiness. Menu curation is not a security sandbox.

QA found and preserved an invalid Neko v6 Home policy: `StartPage:"home"`. V7 changes it to the supported `"homepage"` value and installs the exact file into Firefox's actual distribution path. Startup, Home and New Tab are checked independently. Ordinary Mac Alt keys have upstream noVNC/Neko translations; explicit Linux SDK chords are labeled emulated rather than physical-key coverage.

Chromium can show a **Restore pages?** bubble after power-off/restart. It visibly obscured the VM A postrestart probe; that failed attempt is retained as a profile/restart UX limitation. QA dismisses it through the viewer before continuing, without inferring a transport failure or silently changing the image.

A disposable NAT clone reported the same 192.168.64.4 address as canonical `nat`; it was immediately stopped before media/input tests. Actual promotion stopped the old NAT before starting the replacement. Simultaneous NAT clone identity remains unvalidated. After each canonical NAT restart, QA compares its actual IP with the LAN forwarder's recorded target before LAN media acceptance.

The failed panel Button3 interception experiment, invalid Home policy, locator/helper failures and original raw evidence remain separate. The final runtime has no custom panel input guard. Previous UX/live-drag/LAN reports remain immutable. Mobile/physical keys and performance retesting are deferred; historical timing is not a benchmark of this new stack.

See [desktop report](../desktop-polish.md), [stack and size rationale](../architecture/desktop-stack.md), [independent verification](../desktop-polish-verification.md) and [VM preparation/rollback](../../pocs/microvm/polish/README.md).
