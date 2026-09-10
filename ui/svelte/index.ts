// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Svelte barrel: every block @bagel/ui ships, under one import.
//
// THE PER-FILE SUBPATHS STAY, and this does not replace them. Each adapter
// JS-imports its own contract stylesheet, which is how a bundler is told to
// emit that CSS; importing the barrel therefore pulls in every stylesheet in
// the package. That is the right trade for the consoles, which render most of
// the library on most routes and already load the whole of it -- and the wrong
// one for a page that wants a single element, which is why
// `@bagel/ui/svelte/Button.svelte` still resolves and why ui/scripts/size.ts
// keeps a per-entry budget for the separation.
//
// Generated shape, hand-maintained content: ui/scripts/gen-catalog.mjs fails
// the check when an adapter exists and is not exported here, so the barrel
// cannot silently fall behind the directory.
//
// Grouped by family, in the order the catalog lists them (ui/CATALOG.md).


// ── Typography ─────────────────────────────────────────────────────────────
export { default as Code } from './Code.svelte';
export { default as Eyebrow } from './Eyebrow.svelte';
export { default as Heading } from './Heading.svelte';
export { default as Kbd } from './Kbd.svelte';
export { default as Label } from './Label.svelte';
export { default as Lead } from './Lead.svelte';
export { default as SectionHeading } from './SectionHeading.svelte';
export { default as Text } from './Text.svelte';
export { default as TextLink } from './TextLink.svelte';
export { default as VisuallyHidden } from './VisuallyHidden.svelte';

// ── Layout ─────────────────────────────────────────────────────────────
export { default as AppShell } from './AppShell.svelte';
export { default as Cluster } from './Cluster.svelte';
export { default as Container } from './Container.svelte';
export { default as Divider } from './Divider.svelte';
export { default as Grid } from './Grid.svelte';
export { default as InspectorSurface } from './InspectorSurface.svelte';
export { default as PageHero } from './PageHero.svelte';
export { default as Scroller } from './Scroller.svelte';
export { default as Section } from './Section.svelte';
export { default as Spacer } from './Spacer.svelte';
export { default as Stack } from './Stack.svelte';

// ── Controls ─────────────────────────────────────────────────────────────
export { default as Button } from './Button.svelte';
export { default as ButtonLink } from './ButtonLink.svelte';
export { default as Checkbox } from './Checkbox.svelte';
export { default as Field } from './Field.svelte';
export { default as FieldError } from './FieldError.svelte';
export { default as IconButton } from './IconButton.svelte';
export { default as Input } from './Input.svelte';
export { default as RadioGroup } from './RadioGroup.svelte';
export { default as SearchInput } from './SearchInput.svelte';
export { default as SegmentedControl } from './SegmentedControl.svelte';
export { default as Select } from './Select.svelte';
export { default as Switch } from './Switch.svelte';
export { default as Textarea } from './Textarea.svelte';
export { default as Toggle } from './Toggle.svelte';

// ── Feedback ─────────────────────────────────────────────────────────────
export { default as AlertBanner } from './AlertBanner.svelte';
export { default as Badge } from './Badge.svelte';
export { default as Chip } from './Chip.svelte';
export { default as ConfirmDialog } from './ConfirmDialog.svelte';
export { default as EmptyState } from './EmptyState.svelte';
export { default as ErrorScene } from './ErrorScene.svelte';
export { default as Modal } from './Modal.svelte';
export { default as SaveStatus } from './SaveStatus.svelte';
export { default as Skeleton } from './Skeleton.svelte';
export { default as SkeletonStack } from './SkeletonStack.svelte';
export { default as Tag } from './Tag.svelte';
export { default as ToastHost } from './ToastHost.svelte';
export { default as Tooltip } from './Tooltip.svelte';

// ── Navigation ─────────────────────────────────────────────────────────────
export { default as Brand } from './Brand.svelte';
export { default as Dock } from './Dock.svelte';
export { default as EditorFooter } from './EditorFooter.svelte';
export { default as Footer } from './Footer.svelte';
export { default as Hamburger } from './Hamburger.svelte';
export { default as LanguageSwitcher } from './LanguageSwitcher.svelte';
export { default as MobileMenu } from './MobileMenu.svelte';
export { default as Nav } from './Nav.svelte';
export { default as NavGroup } from './NavGroup.svelte';
export { default as NavLink } from './NavLink.svelte';
export { default as PageHead } from './PageHead.svelte';
export { default as PageToolbar } from './PageToolbar.svelte';
export { default as Rail } from './Rail.svelte';
export { default as RailItem } from './RailItem.svelte';
export { default as SectionNav } from './SectionNav.svelte';
export { default as SocialRail } from './SocialRail.svelte';
export { default as Topbar } from './Topbar.svelte';

// ── Data ─────────────────────────────────────────────────────────────
export { default as AreaSeries } from './AreaSeries.svelte';
export { default as Card } from './Card.svelte';
export { default as CardHead } from './CardHead.svelte';
export { default as DeckList } from './DeckList.svelte';
export { default as Icon } from './Icon.svelte';
export { default as ManagementRow } from './ManagementRow.svelte';
export { default as OverviewGrid } from './OverviewGrid.svelte';
export { default as StatTile } from './StatTile.svelte';
export { default as Table } from './Table.svelte';

// ── Motion ─────────────────────────────────────────────────────────────
export { default as AuroraBg } from './AuroraBg.svelte';
export { default as BackgroundOrbs } from './BackgroundOrbs.svelte';
export { default as Brackets } from './Brackets.svelte';
export { default as CardAtmosphere } from './CardAtmosphere.svelte';
export { default as Cursor } from './Cursor.svelte';
export { default as LightField } from './LightField.svelte';
export { default as ReadingProgress } from './ReadingProgress.svelte';

// ── Stores, actions and the non-component surface ───────────────────────────
// Re-exported so a consumer that took the barrel does not then need to know
// which of these live in a `.svelte.ts` and which in a plain module.
export { toast, toasts, dismissToast, type ToastItem } from './toast.svelte';
export * from './inspector.svelte';
export * from './discard-guard.svelte';
export { reveal, decode, magnetic } from './actions';
