# Sidekicks shared desktop assets

Owner: integration lead. Original code-native SVG art and light home page, no OpenAI affiliation/proprietary assets. wallpaper.svg is1280×720 with a bright cyan/blue gradient; PNG exported from the same vector. Three generic app icons are available as SVG and256px PNG.

Install copies read-only into each backend build context. Home has root data-sidekicks-home=sky-v1, buttons data-app=browser|terminal|files, diagnostics link /fixture, and no fake window controls. Internal GET/home replaces __SIDEKICKS_DESKTOP_TOKEN__ with a perboot capability. POST/desktop/launch takes fixed JSON {app}, header X-Sidekicks-Desktop, exact guest-loopback Host/Origin and source127.0.0.1, and active ownership. Host-forwarded VM connections originate127.0.0.2 and must never receive a launch capability. No arbitrary paths/commands.

GET/desktop/state is fixture-independent desktop readiness. Default home must not overwrite the synthetic fixture. Native app controls, minimize/restore/reopen, pointer and fullscreen are guest/viewer responsibilities and require live QA; these assets alone prove none of them. Clock follows the user's specified America/New_York timezone.
