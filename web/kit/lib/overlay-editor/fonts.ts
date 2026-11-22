// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const googleFonts = [
  'Archivo', 'Barlow', 'Bricolage Grotesque', 'Cormorant Garamond', 'DM Sans', 'DM Serif Display',
  'Fjalla One', 'Geist', 'IBM Plex Mono', 'Instrument Sans', 'Instrument Serif', 'Inter', 'Lato',
  'Lexend', 'Libre Baskerville', 'Merriweather', 'Montserrat', 'Nunito', 'Open Sans', 'Oswald',
  'Outfit', 'Playfair Display', 'Poppins', 'Quicksand', 'Raleway', 'Roboto', 'Rubik', 'Space Grotesk',
  'Spectral', 'Work Sans'
] as const;

export const fontFamilies: readonly string[] = ['System sans', 'System serif', 'System mono', ...googleFonts];
