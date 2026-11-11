#!/usr/bin/env sh
# Regenerates docs/assets/airval-tour.gif and the README stills from a fresh, seeded instance.
# Never touches a real deployment: it starts its own `airval dev` in a temp dir on a spare port.
# Needs: make build, python3 with Pillow, ffmpeg, Playwright chromium (npm --prefix web ci).
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${PORT:-8399}
BASE="http://127.0.0.1:$PORT"
WORK=$(mktemp -d)
OUT="$WORK/frames"
mkdir -p "$OUT"
trap 'kill "$PID" 2>/dev/null || true; rm -rf "$WORK"' EXIT

start() { (cd "$WORK" && exec "$ROOT/bin/airval" dev --listen "127.0.0.1:$PORT" >"$WORK/dev.log" 2>&1) & PID=$!; sleep 3; }

[ -x "$ROOT/bin/airval" ] || make -C "$ROOT" build
start
python3 -I "$ROOT/scripts/readme-media/seed.py" "$BASE" >/dev/null
kill "$PID"; wait "$PID" 2>/dev/null || true
python3 -I "$ROOT/scripts/readme-media/backdate.py" "$WORK/airval-data/airval.db"
start
(cd "$ROOT/web" && node scripts/readme-capture.mjs "$BASE" "$OUT")

# GIF: ~2.6 s per screen, 0.4 s crossfades, 12 fps, 1100 px wide, palette tuned to the frames.
python3 -I - "$OUT" "$ROOT/docs/assets" <<'PY'
import glob, subprocess, sys
from PIL import Image
src, dst = sys.argv[1], sys.argv[2]
frames = sorted(glob.glob(f"{src}/*.png"))
hold, fade = 2.6, 0.4
args, chain, prev, offset = [], [], "0:v", 0.0
for f in frames:
    args += ["-loop", "1", "-t", str(hold), "-i", f]
for i in range(1, len(frames)):
    offset += hold - fade
    chain.append(f"[{prev}][{i}:v]xfade=transition=fade:duration={fade}:offset={offset:.2f}[x{i}]")
    prev = f"x{i}"
chain.append(f"[{prev}]fps=12,scale=1100:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=192:stats_mode=full[p];"
             "[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle")
subprocess.run(["ffmpeg", "-y", "-loglevel", "error", *args, "-filter_complex", ";".join(chain), "-loop", "0",
                f"{dst}/airval-tour.gif"], check=True)
for name, out in [("03-run-detail", "run-detail"), ("06-compare", "compare"), ("10-run-detail-dark", "run-detail-dark")]:
    im = Image.open(f"{src}/{name}.png").convert("RGB").quantize(colors=256, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE)
    im.save(f"{dst}/{out}.png", optimize=True)
PY
echo "Updated docs/assets/airval-tour.gif and stills."
