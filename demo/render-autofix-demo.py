#!/usr/bin/env python3
"""Render the short autofix demo GIF and MP4 without a terminal recorder."""

from __future__ import annotations

import shutil
import subprocess
import tempfile
import textwrap
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parent
WIDTH = 1200
HEIGHT = 720
FPS = 12
FONT_SIZE = 22
LINE_HEIGHT = 30
LEFT = 48
TOP = 82
COLS = 92

BG = "#0a0a0a"
TERM = "#1e1e2e"
TEXT = "#cdd6f4"
MUTED = "#7f849c"
GREEN = "#a6e3a1"
YELLOW = "#f9e2af"
RED = "#f38ba8"
BLUE = "#89b4fa"


def font(candidates: list[str], size: int) -> ImageFont.FreeTypeFont:
    for candidate in candidates:
        path = Path(candidate)
        try:
            return ImageFont.truetype(str(path), size)
        except OSError:
            continue
    return ImageFont.load_default(size=size)


REGULAR = font(
    [
        "C:/Windows/Fonts/consola.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
        "/Library/Fonts/Menlo.ttc",
    ],
    FONT_SIZE,
)
BOLD = font(
    [
        "C:/Windows/Fonts/consolab.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
        "/Library/Fonts/Menlo.ttc",
    ],
    FONT_SIZE,
)
TITLE = font(
    [
        "C:/Windows/Fonts/consolab.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
        "/Library/Fonts/Menlo.ttc",
    ],
    18,
)


def wrapped(lines: list[str]) -> list[str]:
    out: list[str] = []
    for line in lines:
        if len(line) <= COLS:
            out.append(line)
            continue
        out.extend(textwrap.wrap(line, width=COLS, subsequent_indent="  "))
    return out


def line_color(line: str) -> str:
    if line.startswith("$"):
        return GREEN
    if line.startswith("x "):
        return RED
    if line.startswith("would fix:"):
        return YELLOW
    if line.startswith("fixed:") or line == "no findings":
        return GREEN
    if line.startswith("llm-lint"):
        return BLUE
    if line.endswith("findings  (3 errors, 0 warnings, 0 info)"):
        return RED
    return TEXT


def draw_frame(lines: list[str]) -> Image.Image:
    img = Image.new("RGB", (WIDTH, HEIGHT), BG)
    d = ImageDraw.Draw(img)
    d.rounded_rectangle((20, 20, WIDTH - 20, HEIGHT - 20), radius=8, fill=TERM)
    d.ellipse((46, 45, 62, 61), fill="#f38ba8")
    d.ellipse((72, 45, 88, 61), fill="#f9e2af")
    d.ellipse((98, 45, 114, 61), fill="#a6e3a1")
    d.text((WIDTH // 2 - 135, 43), "llm-lint autofix preview", font=TITLE, fill=MUTED)

    visible = wrapped(lines)[-19:]
    y = TOP
    for line in visible:
        if line.startswith("$ "):
            d.text((LEFT, y), "$", font=BOLD, fill=GREEN)
            d.text((LEFT + 26, y), line[2:], font=REGULAR, fill=TEXT)
        else:
            d.text(
                (LEFT, y),
                line,
                font=BOLD if line.startswith(("x ", "would fix:", "fixed:")) else REGULAR,
                fill=line_color(line),
            )
        y += LINE_HEIGHT
    return img


def timeline() -> list[tuple[float, list[str]]]:
    scenes: list[tuple[float, list[str]]] = []
    lines: list[str] = []

    def hold(seconds: float) -> None:
        scenes.append((seconds, lines.copy()))

    def type_command(command: str, seconds: float) -> None:
        steps = max(1, int(seconds * FPS))
        for i in range(1, steps + 1):
            n = int(len(command) * i / steps)
            scenes.append((1 / FPS, lines + ["$ " + command[:n]]))
        lines.append("$ " + command)

    type_command("git log -1 --pretty=%B", 0.8)
    lines.extend(["feat: ship the thing", "", "Co-authored-by: Claude <noreply@anthropic.com>", ""])
    hold(0.8)

    type_command("llm-lint scan --fix-preview --fix-git-history latest", 1.6)
    lines.extend(
        [
            "llm-lint 0.3.1  scanned 2 files + 1 commits in 15ms",
            "",
            "x LLM001  error    CLAUDE.md committed",
            "  -> CLAUDE.md",
            "x LLM003  error    Co-authored-by: Claude trailer",
            "  -> commit 2613325 \"feat: ship the thing\"",
            "x LLM006  error    .cursorrules / .cursor/ tracked",
            "  -> .cursorrules",
            "",
            "3 findings  (3 errors, 0 warnings, 0 info)",
            "would fix: 1 files changed, 1 commit messages cleaned, 4 .gitignore entries added, 2 index entries untracked",
            "",
        ]
    )
    hold(2.4)

    type_command("llm-lint scan --fix --fix-git-history latest", 1.3)
    lines.extend(
        [
            "llm-lint 0.3.1  scanned 1 files + 1 commits in 9ms",
            "",
            "fixed: 1 files changed, 1 commit messages cleaned, 4 .gitignore entries added, 2 index entries untracked",
            "",
        ]
    )
    hold(1.5)

    type_command("llm-lint scan", 0.7)
    lines.extend(["llm-lint 0.3.1  scanned 1 files + 1 commits in 1ms", "", "no findings"])
    hold(2.0)
    return scenes


def main() -> None:
    ffmpeg = shutil.which("ffmpeg")
    if ffmpeg is None:
        ffmpeg = "C:/Program Files/WinGet/Links/ffmpeg.exe"
    if not ffmpeg:
        raise SystemExit("ffmpeg is required")

    frames: list[Image.Image] = []
    for seconds, lines in timeline():
        count = max(1, int(seconds * FPS))
        frame = draw_frame(lines)
        frames.extend(frame.copy() for _ in range(count))

    with tempfile.TemporaryDirectory(prefix="llm-lint-demo-frames-", dir=ROOT) as tmp:
        frame_dir = Path(tmp)
        for i, frame in enumerate(frames):
            frame.save(frame_dir / f"frame-{i:04d}.png")

        palette = frame_dir / "palette.png"
        subprocess.run(
            [
                ffmpeg,
                "-y",
                "-framerate",
                str(FPS),
                "-i",
                str(frame_dir / "frame-%04d.png"),
                "-vf",
                "palettegen=stats_mode=diff",
                str(palette),
            ],
            check=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        subprocess.run(
            [
                ffmpeg,
                "-y",
                "-framerate",
                str(FPS),
                "-i",
                str(frame_dir / "frame-%04d.png"),
                "-i",
                str(palette),
                "-lavfi",
                "paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle",
                str(ROOT / "demo.gif"),
            ],
            check=True,
        )
        subprocess.run(
            [
                ffmpeg,
                "-y",
                "-framerate",
                str(FPS),
                "-i",
                str(frame_dir / "frame-%04d.png"),
                "-c:v",
                "libx264",
                "-pix_fmt",
                "yuv420p",
                "-movflags",
                "+faststart",
                str(ROOT / "demo.mp4"),
            ],
            check=True,
        )


if __name__ == "__main__":
    main()
