# Millena IQ

Product prototype for automated content operations across social media, blog,
newsletter and website publishing.

The prototype includes Croatian and English interfaces, with Croatian as the
default language. It runs as a static app without a build step.

Planned product domain: `millena.ai`.

## Product flows

- project onboarding with optional strategy upload or guided strategy setup
- audience, content themes, voice and publishing cadence
- synchronized Telegram and WhatsApp bot intake, editing and approvals
- direct app workflow with shared admin visibility and conversation history
- social content studio with channel-specific drafts and scheduling
- component-based blog editor with SEO and newsletter handoff
- weekly or monthly newsletter preparation and recipient management
- website subscriptions, manual contacts and CSV imports
- LinkedIn, Instagram, Facebook, YouTube, X, Reddit, Pinterest and Threads
- social, website, newsletter and custom API connections
- per-channel review and automatic publishing rules
- website integration and optional Millena website package

## Files

- `index.html` - public Millena IQ product website
- `login.html` - bilingual sign-in and project entry screen
- `app.html` - authenticated application screens and product workflows
- `site.css` / `site.js` - public website and login presentation/behavior
- `styles.css` - responsive product interface and visual system
- `script.js` - navigation, onboarding, language switching and interactions
- `assets/lucide.min.js` - local Lucide icon runtime
- `assets/millena-mark.svg` - Millena companion brand mark
- `assets/` - supporting visual assets

Open `index.html` directly in a browser. The demo login establishes a local
browser session and opens `app.html`; signing out returns to the public site.
No build step is required.
