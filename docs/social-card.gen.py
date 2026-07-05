#!/usr/bin/env python3
"""Generate the side-quest social preview card (docs/social-card.png), SQ-0061.

Emits the card SVG to stdout. Source-code backdrop is read from the repo, so
run it from anywhere — paths resolve relative to this file. Regenerate with:

    python3 docs/social-card.gen.py > docs/social-card.svg
    rsvg-convert -w 1280 -h 640 docs/social-card.svg -o docs/social-card.png

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

# --- recursive mark: a pin whose '!' stem is a smaller pin (Droste) --------
# n = number of pins (outer + inner). n=3 -> outer + two inner marks.
K = (5 ** 0.5 - 1) / 2   # 1/phi ~ 0.618: each inner pin is a golden-ratio step down
CY = 25.0                # nested-pin centre (parent space)

def mark(n):
    pin_fill = ORANGE if n % 2 == 1 else INK
    ink_fill = INK if n % 2 == 1 else ORANGE
    parts = [f'<path fill="{pin_fill}" d="{PIN}"/>']
    if n > 1:
        tx, ty = 32 - 32 * K, CY - 32 * K
        parts.append(f'<g transform="translate({tx:.3f},{ty:.3f}) scale({K})">'
                     f'{mark(n-1)}</g>')
    else:
        # only the innermost pin carries a '!' (bar + dot); no trailing dots
        parts.append(f'<path fill="{ink_fill}" d="{BANG}"/>')
        parts.append(f'<circle fill="{ink_fill}" cx="32" cy="37.4" r="3"/>')
    return "".join(parts)

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

code = col("internal/store/store.go", 0, 22, 40) + "\n" + \
       col("internal/merge/merge.go", 0, 22, 700)

svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}">
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

  <!-- recursive mark, above the 'side' glyphs -->
  <g transform="translate(438,214) scale(3.35)">
    <g transform="translate(-32,-32)">
      {mark(3)}
    </g>
  </g>

  <!-- wordmark -->
  <text x="640" y="452" text-anchor="middle" font-family="Georgia, serif"
        font-weight="bold" font-size="118" fill="{WORD}">side-quest</text>

  <!-- catchphrase -->
  <text x="640" y="524" text-anchor="middle" font-family="Menlo, monospace"
        font-size="27" fill="{TAG}" letter-spacing="0.5">Zero overhead issue tracking, keeping you in flow.</text>
</svg>'''

sys.stdout.write(svg)
