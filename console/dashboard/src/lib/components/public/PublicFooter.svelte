<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary and unlicensed. See LICENSE.md.
  // The marketing site's footer (web/src/components/layout/Footer.astro), as it
  // was first converted for routes/user/[userId]: sign-off, brand + three link
  // columns, legal strip. Extracted here so every public page signs off the same
  // way; copy comes from the i18n catalog and the site links follow the
  // visitor's locale.
  import { getI18n } from '@bagel/shared';
  import { dashHref, webHref } from './links';

  const { t, locale } = getI18n();
</script>

<footer class="site-footer">
  <div class="signoff">
    <p class="signoff__line">{t('public.footer.signoff')}</p>
    <p class="signoff__sub">{t('public.footer.signoffSub')}</p>
  </div>

  <div class="foot-top">
    <div class="foot-brand">
      <span class="foot-brand__logo"
        ><img src="/logo.png" alt={t('public.footer.logoAlt')} width="55" height="55" /></span
      >
      <div>
        <span class="foot-name">ItsBagelBot</span>
        <span class="foot-tagline">{t('public.footer.tagline')}</span>
      </div>
    </div>

    <div class="foot-cols">
      <div class="foot-col">
        <p>{t('public.footer.product')}</p>
        <a href={webHref('/pricing', locale)}>{t('public.footer.pricing')}</a>
        <a href={webHref('/guides', locale)}>{t('public.footer.guides')}</a>
        <a href={webHref('/command-builder', locale)}>{t('public.footer.builder')}</a>
        <a href="https://docs.itsbagelbot.com" target="_blank" rel="noopener noreferrer"
          >{t('public.footer.developer')}</a
        >
        <a href={dashHref(locale)} target="_blank" rel="noopener noreferrer">{t('public.footer.dashboard')}</a>
      </div>
      <div class="foot-col">
        <p>{t('public.footer.company')}</p>
        <a href={webHref('/contact', locale)}>{t('public.footer.contact')}</a>
        <a href="https://github.com/AdamOusmer/ItsBagelBot" target="_blank" rel="noopener noreferrer">GitHub</a>
      </div>
      <div class="foot-col">
        <p>{t('public.footer.community')}</p>
        <a href="https://discord.gg/SZ2remwSDv" target="_blank" rel="noopener noreferrer">Discord</a>
        <a href="https://twitch.tv/itsbagelbot" target="_blank" rel="noopener noreferrer">Twitch</a>
      </div>
    </div>
  </div>

  <div class="foot-bottom">
    <span class="foot-copy">{t('public.footer.copyright')}</span>
    <span class="foot-note">{t('public.footer.note')}</span>
    <div class="foot-legal">
      <a href={webHref('/privacy', locale)}>{t('public.footer.privacy')}</a>
      <a href={webHref('/terms', locale)}>{t('public.footer.terms')}</a>
      <a href={webHref('/creator-terms', locale)}>{t('public.footer.creatorTerms')}</a>
    </div>
  </div>
</footer>

<style>
  .site-footer {
    position: relative;
    border-top: 1px solid var(--bb-border);
    padding: 72px 4% 40px;
    overflow: hidden;
    color: var(--bb-white);
    /* web/'s footer sits on the body's solid ink; here fixed background
       canvases (aurora, starfield) run the whole page, so the footer paints
       its own ground to occlude them the same way. */
    background: var(--bb-bg-0);
  }
  .site-footer::before {
    content: "";
    position: absolute;
    left: 50%;
    top: 0;
    width: 90vmin;
    height: 40vmin;
    translate: -50% -50%;
    pointer-events: none;
    background: radial-gradient(50% 60% at 50% 50%, rgba(201,168,124,0.09) 0%, transparent 70%);
    filter: blur(12px);
  }
  .signoff { position: relative; text-align: center; margin-bottom: 72px; }
  .signoff__line {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(1.7rem, 4.6vw, 2.9rem);
    letter-spacing: -0.02em;
    line-height: 1.08;
    color: var(--bb-white);
    margin: 0 0 10px;
    text-shadow: 0 0 30px rgba(201,168,124,0.16);
  }
  .signoff__sub {
    font-family: var(--bb-font-hand, "Caveat", cursive);
    font-size: clamp(1.2rem, 2.6vw, 1.5rem);
    font-weight: 500;
    color: var(--bb-tan-light);
    margin: 0;
    rotate: -1deg;
  }
  .foot-top {
    position: relative;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 60px;
    margin-bottom: 60px;
  }
  .foot-brand { display: flex; align-items: center; gap: 15px; }
  .foot-brand__logo {
    display: inline-block;
    line-height: 0;
    transition: rotate 500ms cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .foot-brand__logo img { width: 55px; height: 55px; border-radius: 10px; }
  @media (hover: hover) and (pointer: fine) {
    .foot-brand:hover .foot-brand__logo { rotate: 14deg; }
  }
  .foot-brand div { display: flex; flex-direction: column; }
  .foot-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 1.1rem;
    color: var(--bb-white);
    margin-bottom: 10px;
  }
  .foot-tagline { font-family: var(--bb-font-mono); font-size: 0.9rem; color: var(--bb-muted); letter-spacing: 0.05em; }
  .foot-cols { display: flex; gap: 60px; }
  .foot-col { display: flex; flex-direction: column; align-items: flex-start; gap: 10px; }
  .foot-col p {
    font-family: var(--bb-font-mono);
    font-size: 0.9rem;
    letter-spacing: 0.10em;
    text-transform: uppercase;
    color: var(--bb-white);
    margin: 0 0 4px 0;
  }
  .foot-col a {
    position: relative;
    width: fit-content;
    font-family: var(--bb-font-body);
    font-size: 0.85rem;
    color: var(--bb-muted);
    text-decoration: none;
    transition: color 0.2s var(--bb-ease-out-expo), transform 0.3s var(--bb-ease-out-expo);
  }
  .foot-col a::before {
    content: "";
    position: absolute;
    left: -12px;
    top: 50%;
    width: 4px;
    height: 4px;
    border-radius: 50%;
    translate: 0 -50%;
    background: var(--bb-tan);
    opacity: 0;
    scale: 0;
    transition: opacity 0.25s ease, scale 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .foot-col a:hover { color: var(--bb-tan-light); transform: translateX(6px); }
  .foot-col a:hover::before { opacity: 1; scale: 1; }
  @media (prefers-reduced-motion: reduce) {
    .foot-col a:hover { transform: none; }
    .foot-col a::before { display: none; }
    .foot-brand__logo { transition: none; }
  }
  .foot-bottom {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    padding-top: 30px;
    border-top: 1px solid rgba(255,255,255,0.05);
  }
  .foot-copy, .foot-note {
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    color: var(--bb-muted);
    letter-spacing: 0.06em;
  }
  .foot-note { letter-spacing: 0.08em; }
  .foot-legal { display: flex; justify-content: flex-end; gap: 20px; }
  .foot-legal a {
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    color: var(--bb-muted);
    text-decoration: none;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    transition: color 0.2s;
  }
  .foot-legal a:hover { color: var(--bb-tan-light); }

  @media (max-width: 900px) {
    .site-footer { padding: 56px 25px 30px; }
    .signoff { margin-bottom: 56px; }
    .foot-top { flex-direction: column; gap: 40px; }
    .foot-cols { flex-wrap: wrap; gap: 30px; }
    .foot-bottom { grid-template-columns: 1fr; gap: 10px; text-align: center; }
    .foot-legal { justify-content: center; }
  }
</style>
