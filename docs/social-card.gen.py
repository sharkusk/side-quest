#!/usr/bin/env python3
"""Generate the side-quest brand assets: the social preview card and the mark.

Both share one definition of the pin, so they cannot drift. The
source-code backdrop is read from the repo; paths resolve relative to this
file, so run it from anywhere. Regenerate with:

    # social card (docs/social-card.png), SQ-0061
    python3 docs/social-card.gen.py > docs/social-card.svg
    rsvg-convert -w 1280 -h 640 docs/social-card.svg -o docs/social-card.png

    # standalone mark (docs/mark.svg)
    python3 docs/social-card.gen.py mark > docs/mark.svg

Fonts used (system): Georgia (wordmark), Menlo (backdrop + catchphrase).
"""
import html, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent  # repo root (docs/..)

W, H = 1280, 640
ORANGE = "#e8862a"
INK = "#23262b"
WORD = "#f2f0ec"
TAG = "#8a9199"

# The brand mark, a location pin containing an exclamation, in a 64x64 space.
PIN = ("M32 4.5c11.9 0 21.5 9.4 21.5 21 0 8.2-6.6 16.3-21.5 34.5"
       "C17.1 41.8 10.5 33.7 10.5 25.5c0-11.6 9.6-21 21.5-21Z")
BANG = "M29.4 13.6h5.2l-1.15 17.3h-2.9Z"

# --- the mark: a location pin with a single '!' in the middle --------------
def mark():
    return (f'<path fill="{ORANGE}" d="{PIN}"/>'
            f'<path fill="{INK}" d="{BANG}"/>'
            f'<circle fill="{INK}" cx="32" cy="37.4" r="3"/>')

# --- source-code backdrop (two columns, top half, faded out below) ---------
def col(rel, start, count, x, width=48):
    out = []
    y = 30
    lines = (ROOT / rel).read_text().splitlines()
    for ln in lines[start:start + count]:
        ln = ln.replace("\t", "  ")[:width]
        out.append(f'<text x="{x}" y="{y}" xml:space="preserve">{html.escape(ln)}</text>')
        y += 25
    return "\n".join(out)

def card_svg():
    code = col("internal/store/store.go", 0, 22, 40) + "\n" + \
           col("internal/merge/merge.go", 0, 22, 700)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}">
  <defs>
    <radialGradient id="glow" cx="35%" cy="70%" r="60%">
      <stop offset="0%" stop-color="#2b2620"/>
      <stop offset="55%" stop-color="#181a1e"/>
      <stop offset="100%" stop-color="#131519"/>
    </radialGradient>
    <linearGradient id="codefade" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#fff" stop-opacity="1"/>
      <stop offset="42%" stop-color="#fff" stop-opacity="1"/>
      <stop offset="70%" stop-color="#fff" stop-opacity="0"/>
    </linearGradient>
    <mask id="topfade"><rect width="{W}" height="{H}" fill="url(#codefade)"/></mask>
  </defs>

  <rect width="{W}" height="{H}" fill="#131519"/>
  <rect width="{W}" height="{H}" fill="url(#glow)"/>

  <!-- dimmed project code: fills the top half, fades into the bottom -->
  <g mask="url(#topfade)" font-family="Menlo, monospace" font-size="15.5"
     fill="{ORANGE}" opacity="0.12">
    {code}
  </g>

  <!-- mark, above the 'side' glyphs -->
  <g transform="translate(438,214) scale(3.35)">
    <g transform="translate(-32,-32)">
      {mark()}
    </g>
  </g>

  <!-- wordmark -->
  <text x="640" y="452" text-anchor="middle" font-family="Georgia, serif"
        font-weight="bold" font-size="118" fill="{WORD}">side-quest</text>

  <!-- catchphrase -->
  <text x="640" y="524" text-anchor="middle" font-family="Menlo, monospace"
        font-size="27" fill="{TAG}" letter-spacing="0.5">Zero overhead issue tracking, keeping you in flow.</text>
</svg>'''

def mark_svg():
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" '
            f'viewBox="0 0 64 64" role="img" aria-label="side-quest">\n'
            f'  {mark()}\n'
            f'</svg>\n')

mode = sys.argv[1] if len(sys.argv) > 1 else "card"
sys.stdout.write(mark_svg() if mode == "mark" else card_svg())
