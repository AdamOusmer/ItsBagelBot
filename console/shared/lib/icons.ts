// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Stroke icons lifted from the design bundle (Dashboard.html). Values are the
// inner markup of a `viewBox="0 0 24 24"` SVG; Icon.svelte wraps them. Static
// constants only — never interpolate user input here.
export const icons = {
  // Discord's official mark, from their brand assets. The only filled glyph in
  // a set of stroke icons, so it carries fill/stroke on the path itself:
  // Icon.svelte puts fill="none" stroke="currentColor" on the <svg>, and a
  // presentation attribute on the child wins, which keeps every existing call
  // site working without passing a fill prop for this one icon.
  discord:
    '<path fill="currentColor" stroke="none" d="M20.317 4.3698a19.7913 19.7913 0 0 0-4.8851-1.5152.0741.0741 0 0 0-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 0 0-.0785-.037 19.7363 19.7363 0 0 0-4.8852 1.515.0699.0699 0 0 0-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 0 0 .0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 0 0 .0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 0 0-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 0 1-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 0 1 .0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 0 1 .0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 0 1-.0066.1276 12.2986 12.2986 0 0 1-1.873.8914.0766.0766 0 0 0-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 0 0 .0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 0 0 .0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 0 0-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189z"/>',
  overview:
    '<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>',
  commands: '<polyline points="4 7 9 12 4 17"/><line x1="12" y1="17" x2="20" y2="17"/>',
  moderation: '<path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6z"/>',
  activity:
    '<path d="M20 12a8 8 0 1 1-3.4-6.5"/><polyline points="20 4 20 9 15 9"/>',
  settings:
    '<line x1="4" y1="7.5" x2="20" y2="7.5"/><circle cx="9.5" cy="7.5" r="2.2"/><line x1="4" y1="16.5" x2="20" y2="16.5"/><circle cx="14.5" cy="16.5" r="2.2"/>',
  lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
  search: '<circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16.5" y2="16.5"/>',
  bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.7 21a2 2 0 0 1-3.4 0"/>',
  check: '<polyline points="20 6 9 17 4 12"/>',
  heart: '<path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78L12 21.23l8.84-8.84a5.5 5.5 0 0 0 0-7.78z"/>',
  gem: '<path d="M7 3h10l4 6-9 12L3 9z"/><path d="M3 9h18"/><path d="M8.5 9L12 3l3.5 6"/><path d="M8.5 9l3.5 12 3.5-12"/>',
  plus: '<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>',
  edit: '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>',
  dots: '<circle cx="5" cy="12" r="1.4"/><circle cx="12" cy="12" r="1.4"/><circle cx="19" cy="12" r="1.4"/>',
  power: '<path d="M18.4 5.6a9 9 0 1 1-12.8 0"/><line x1="12" y1="2" x2="12" y2="12"/>',
  users:
    '<path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.9"/><path d="M16 3.1a4 4 0 0 1 0 7.8"/>',
  pulse: '<path d="M3 12h4l3 8 4-16 3 8h4"/>',
  send: '<path d="M3 11l19-9-9 19-2-8-8-2z"/>',
  ban: '<circle cx="12" cy="12" r="9"/><line x1="5.6" y1="5.6" x2="18.4" y2="18.4"/>',
  clock: '<circle cx="12" cy="12" r="9"/><polyline points="12 7 12 12 15 14"/>',
  trash: '<path d="M4 7h16"/><path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/><path d="M6 7l1 13h10l1-13"/>',
  caps: '<path d="M4 7V5a1 1 0 0 1 1-1h14a1 1 0 0 1 1 1v2"/><path d="M9 20h6"/><path d="M12 4v16"/>',
  link: '<path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1"/><path d="M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1"/>',
  blocked: '<circle cx="12" cy="12" r="9"/><line x1="9" y1="9" x2="15" y2="15"/><line x1="15" y1="9" x2="9" y2="15"/>',
  symbol: '<rect x="3" y="11" width="18" height="10" rx="2"/><path d="M7 11V8a5 5 0 0 1 10 0v3"/>',
  follower: '<path d="M3 3v5h5"/><path d="M3.05 13a9 9 0 1 0 .5-4.6L3 8"/>',
  x: '<line x1="6" y1="6" x2="18" y2="18" stroke="currentColor" stroke-width="2" stroke-linecap="round"/><line x1="18" y1="6" x2="6" y2="18" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5.5 9.5V20a1 1 0 0 0 1 1h11a1 1 0 0 0 1-1V9.5"/><path d="M9.5 21v-6h5v6"/>',
  modules:
    '<rect x="4" y="4" width="7" height="7" rx="1.5"/><rect x="13" y="4" width="7" height="7" rx="1.5"/><rect x="4" y="13" width="7" height="7" rx="1.5"/><line x1="16.5" y1="13.5" x2="16.5" y2="19.5"/><line x1="13.5" y1="16.5" x2="19.5" y2="16.5"/>',
  music:
    '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
  lanes:
    '<path d="M5 4v16"/><path d="M19 4v16"/><path d="M12 4v3.5"/><path d="M12 10.2v3.6"/><path d="M12 16.5V20"/>',
  audit:
    '<path d="M14.5 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7.5z"/><path d="M14.5 3v4.5H19"/><line x1="9" y1="12.5" x2="15" y2="12.5"/><line x1="9" y1="16" x2="13" y2="16"/>',
  card: '<rect x="2.5" y="5.5" width="19" height="13" rx="2"/><line x1="2.5" y1="10" x2="21.5" y2="10"/><line x1="6" y1="15" x2="10" y2="15"/>',
  server:
    '<rect x="3.5" y="4" width="17" height="7" rx="1.5"/><rect x="3.5" y="13" width="17" height="7" rx="1.5"/><line x1="7" y1="7.5" x2="7.01" y2="7.5"/><line x1="7" y1="16.5" x2="7.01" y2="16.5"/><line x1="10.5" y1="7.5" x2="13" y2="7.5"/><line x1="10.5" y1="16.5" x2="13" y2="16.5"/>',
  list: '<line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="5" cy="6" r="1"/><circle cx="5" cy="12" r="1"/><circle cx="5" cy="18" r="1"/>',
  quote:
    '<path d="M10 9H6.5A2.5 2.5 0 0 0 4 11.5V15h6z"/><path d="M10 15c0 2.5-1.5 4-4 4.5"/><path d="M20 9h-3.5A2.5 2.5 0 0 0 14 11.5V15h6z"/><path d="M20 15c0 2.5-1.5 4-4 4.5"/>',
  gamepad: '<rect x="2" y="8" width="20" height="9" rx="4.5"/><line x1="6" y1="11" x2="6" y2="14"/><line x1="4.5" y1="12.5" x2="7.5" y2="12.5"/><circle cx="16.5" cy="11.5" r="1.1"/><circle cx="18.5" cy="14" r="1.1"/>',
  coin: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><line x1="12" y1="9.5" x2="12" y2="14.5"/>',
  globe:
    '<circle cx="12" cy="12" r="9"/><line x1="3" y1="12" x2="21" y2="12"/><path d="M12 3a13.8 13.8 0 0 1 3.6 9 13.8 13.8 0 0 1-3.6 9 13.8 13.8 0 0 1-3.6-9A13.8 13.8 0 0 1 12 3z"/>',
  // Module-index glyphs: one shape per job so five stats packs are not the
  // same gamepad and Channel Points is not the same gem as Raffle.
  hex: '<path d="M12 2.5 20 7v10l-8 4.5L4 17V7z"/>',
  tally:
    '<line x1="6" y1="4" x2="6" y2="20"/><line x1="11" y1="4" x2="11" y2="20"/><line x1="16" y1="4" x2="16" y2="20"/><line x1="3.5" y1="8" x2="20.5" y2="16"/>',
  dice:
    '<rect x="4" y="4" width="16" height="16" rx="3"/><circle cx="9" cy="9" r="1.2"/><circle cx="15" cy="9" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="9" cy="15" r="1.2"/><circle cx="15" cy="15" r="1.2"/>',
  swords:
    '<polyline points="6 20 14 4 18 8"/><polyline points="18 20 10 4 6 8"/>',
  chat:
    '<path d="M5 5h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H9l-4 4V7a2 2 0 0 1 2-2z"/><line x1="8" y1="9.5" x2="16" y2="9.5"/><line x1="8" y1="13" x2="13" y2="13"/>',
  smile:
    '<circle cx="12" cy="12" r="9"/><path d="M8 14.5s1.6 2.2 4 2.2 4-2.2 4-2.2"/><circle cx="9" cy="10" r="1"/><circle cx="15" cy="10" r="1"/>',
  megaphone:
    '<path d="M4 10v4a1 1 0 0 0 1 1h1.4L12 19V5L6.4 9H5a1 1 0 0 0-1 1z"/><path d="M16.4 8.2a5.4 5.4 0 0 1 0 7.6"/>',
  // Isometric voxel, not furniture: a bed next to the other stats glyphs
  // looked like a hotel, not Hypixel Bedwars. 2×2 on the top face is what
  // makes an 18px stroke cube read as a Minecraft dirt block rather than a
  // shipping box; side faces get one split so they match without filling in.
  dirt:
    '<path d="M12 3.5 21 8 12 12.5 3 8z"/><path d="M3 8v8l9 4.5V12.5z"/><path d="M21 8v8l-9 4.5V12.5z"/><line x1="7.5" y1="5.75" x2="16.5" y2="10.25"/><line x1="16.5" y1="5.75" x2="7.5" y2="10.25"/><line x1="3" y1="12" x2="12" y2="16.5"/><line x1="21" y1="12" x2="12" y2="16.5"/>',
  pickaxe:
    '<path d="M14 3.5 21 9l-2.2 1.3-5.2-4.2z"/><line x1="12.5" y1="8" x2="5" y2="20"/>',
  parachute:
    '<path d="M4 11c0-4.4 3.6-8 8-8s8 3.6 8 8"/><line x1="4" y1="11" x2="12" y2="18"/><line x1="20" y1="11" x2="12" y2="18"/><line x1="12" y1="11" x2="12" y2="18"/><circle cx="12" cy="20" r="1.4"/>',
  crown:
    '<path d="M3 16 6 7l6 5 6-5 3 9"/><line x1="3" y1="16" x2="21" y2="16"/><line x1="5" y1="19" x2="19" y2="19"/>',
  crosshair:
    '<circle cx="12" cy="12" r="7"/><line x1="12" y1="2" x2="12" y2="7"/><line x1="12" y1="17" x2="12" y2="22"/><line x1="2" y1="12" x2="7" y2="12"/><line x1="17" y1="12" x2="22" y2="12"/>',
  ticket:
    '<path d="M3 9a2.4 2.4 0 0 0 0 6v4h18v-4a2.4 2.4 0 0 0 0-6V5H3z"/><line x1="13" y1="5" x2="13" y2="8"/><line x1="13" y1="16" x2="13" y2="19"/>',
  bulb: '<path d="M9 18h6"/><path d="M10 21h4"/><path d="M12 3a6.4 6.4 0 0 1 4.4 11V16H7.6v-2A6.4 6.4 0 0 1 12 3z"/>',
  // Concentric broadcast waves, not the pulse polyline (Activity) or the
  // megaphone (alerts): Stream Management is "this channel is live and editable",
  // and those two already sit on other catalog tiles.
  broadcast:
    '<circle cx="12" cy="18" r="1.6"/><path d="M8.2 14.2a5.4 5.4 0 0 1 7.6 0"/><path d="M5.2 11.2a9.4 9.4 0 0 1 13.6 0"/><line x1="12" y1="18" x2="12" y2="9"/>'
} as const;

export type IconName = keyof typeof icons;
