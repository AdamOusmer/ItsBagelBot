// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Astro barrel: every block @bagel/ui ships, under one import.
//
// THE PER-FILE SUBPATHS STAY, and this does not replace them. Each adapter
// JS-imports its own contract stylesheet, which is how a bundler is told to
// emit that CSS; importing the barrel therefore pulls in every stylesheet in
// the package. That is the right trade for the consoles, which render most of
// the library on most routes and already load the whole of it -- and the wrong
// one for a page that wants a single element, which is why
// `@bagel/ui/astro/Button.astro` still resolves and why ui/scripts/size.ts
// keeps a per-entry budget for the separation.
//
// Generated shape, hand-maintained content: ui/scripts/gen-catalog.mjs fails
// the check when an adapter exists and is not exported here, so the barrel
// cannot silently fall behind the directory.
//
// Grouped by family, in the order the catalog lists them (ui/CATALOG.md).


// ── Typography ─────────────────────────────────────────────────────────────
export { default as Code } from './Code.astro';
export { default as Eyebrow } from './Eyebrow.astro';
export { default as Heading } from './Heading.astro';
export { default as Kbd } from './Kbd.astro';
export { default as Label } from './Label.astro';
export { default as Lead } from './Lead.astro';
export { default as SectionHeading } from './SectionHeading.astro';
export { default as Text } from './Text.astro';
export { default as TextLink } from './TextLink.astro';
export { default as VisuallyHidden } from './VisuallyHidden.astro';

// ── Layout ─────────────────────────────────────────────────────────────
export { default as AppShell } from './AppShell.astro';
export { default as Cluster } from './Cluster.astro';
export { default as Container } from './Container.astro';
export { default as Divider } from './Divider.astro';
export { default as Grid } from './Grid.astro';
export { default as InspectorSurface } from './InspectorSurface.astro';
export { default as PageHero } from './PageHero.astro';
export { default as Scroller } from './Scroller.astro';
export { default as Section } from './Section.astro';
export { default as Spacer } from './Spacer.astro';
export { default as Stack } from './Stack.astro';

// ── Controls ─────────────────────────────────────────────────────────────
export { default as Button } from './Button.astro';
export { default as ButtonLink } from './ButtonLink.astro';
export { default as Checkbox } from './Checkbox.astro';
export { default as Field } from './Field.astro';
export { default as IconButton } from './IconButton.astro';
export { default as Input } from './Input.astro';
export { default as RadioGroup } from './RadioGroup.astro';
export { default as SearchInput } from './SearchInput.astro';
export { default as SegmentedControl } from './SegmentedControl.astro';
export { default as Select } from './Select.astro';
export { default as Switch } from './Switch.astro';
export { default as Textarea } from './Textarea.astro';

// ── Feedback ─────────────────────────────────────────────────────────────
export { default as AlertBanner } from './AlertBanner.astro';
export { default as Badge } from './Badge.astro';
export { default as Chip } from './Chip.astro';
export { default as EmptyState } from './EmptyState.astro';
export { default as ErrorScene } from './ErrorScene.astro';
export { default as Modal } from './Modal.astro';
export { default as SaveStatus } from './SaveStatus.astro';
export { default as Skeleton } from './Skeleton.astro';
export { default as SkeletonStack } from './SkeletonStack.astro';
export { default as Tag } from './Tag.astro';
export { default as Tooltip } from './Tooltip.astro';

// ── Navigation ─────────────────────────────────────────────────────────────
export { default as Brand } from './Brand.astro';
export { default as Dock } from './Dock.astro';
export { default as EditorFooter } from './EditorFooter.astro';
export { default as Footer } from './Footer.astro';
export { default as Hamburger } from './Hamburger.astro';
export { default as LanguageSwitcher } from './LanguageSwitcher.astro';
export { default as MobileMenu } from './MobileMenu.astro';
export { default as Nav } from './Nav.astro';
export { default as NavGroup } from './NavGroup.astro';
export { default as NavLink } from './NavLink.astro';
export { default as PageHead } from './PageHead.astro';
export { default as PageToolbar } from './PageToolbar.astro';
export { default as Rail } from './Rail.astro';
export { default as RailItem } from './RailItem.astro';
export { default as SectionNav } from './SectionNav.astro';
export { default as SocialRail } from './SocialRail.astro';
export { default as Topbar } from './Topbar.astro';

// ── Data ─────────────────────────────────────────────────────────────
export { default as AreaSeries } from './AreaSeries.astro';
export { default as Card } from './Card.astro';
export { default as CardHead } from './CardHead.astro';
export { default as DeckList } from './DeckList.astro';
export { default as Icon } from './Icon.astro';
export { default as ManagementRow } from './ManagementRow.astro';
export { default as OverviewGrid } from './OverviewGrid.astro';
export { default as StatTile } from './StatTile.astro';
export { default as Table } from './Table.astro';

// ── Motion ─────────────────────────────────────────────────────────────
export { default as AuroraBg } from './AuroraBg.astro';
export { default as BackgroundOrbs } from './BackgroundOrbs.astro';
export { default as Brackets } from './Brackets.astro';
export { default as CardAtmosphere } from './CardAtmosphere.astro';
export { default as Cursor } from './Cursor.astro';
export { default as LightField } from './LightField.astro';
export { default as ReadingProgress } from './ReadingProgress.astro';
