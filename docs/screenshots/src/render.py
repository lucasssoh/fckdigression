#!/usr/bin/env python3
"""Renders the README screenshots (2400x1350) with headless Firefox.

  phones.png    the app on the phone, redrawn from MainActivity (same colors, text and layout)
  terminal.png  the real TUI output, from tui-*.ansi

Regenerate the .ansi files first:
  DUMP_SCREENS=docs/screenshots/src go test ./internal/tui -run TestDumpScreens
Then: python3 docs/screenshots/src/render.py   (needs Firefox)
"""
import html
import os
import re
import shutil
import subprocess
import sys
import tempfile

SRC = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.dirname(SRC)

FONTS = '<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Roboto:wght@400;500&family=Roboto+Mono&display=swap">'

BASE_CSS = """
* { box-sizing: border-box; margin: 0; }
html, body { width: 1200px; height: 675px; overflow: hidden; }
body { background: radial-gradient(120% 90% at 20% 10%, #EEF6F3 0%, #DCEBE6 55%, #CFE3DD 100%);
       font-family: Roboto, sans-serif; position: relative; }
"""

ICON = """<svg viewBox="0 0 108 108" width="{s}" height="{s}" style="border-radius:50%;display:block">
<rect width="108" height="108" fill="#0F5C5A"/>
<path d="M22 54c3.5-13 10-13 13 0s9.5 13 13 0" fill="none" stroke="#F2FBFA" stroke-opacity=".6" stroke-width="6.5" stroke-linecap="round"/>
<path d="M48 54h24" stroke="#E3A93B" stroke-width="6.5" stroke-linecap="round"/>
<circle cx="80" cy="54" r="7" fill="#E3A93B"/></svg>"""

# --------------------------------------------------------------------------- phones

PHONE_CSS = """
.phone { position: absolute; width: 380px; height: 800px; border-radius: 56px; background: #151B1A;
         padding: 12px; box-shadow: 0 30px 60px rgba(15,60,55,.25), 0 6px 14px rgba(15,60,55,.18);
         transform-origin: top left; }
.screen { width: 100%; height: 100%; border-radius: 45px; overflow: hidden; display: flex; flex-direction: column;
          background: var(--bg); color: var(--fg); }
.light { --bg:#F6F8F7; --fg:#142120; --muted:#5D6B69; --card:#FFFFFF; --line:#DEE5E3; --accent:#0F5C5A; --onaccent:#FFF;
         --pillon:#DCEBE9; --pillonfg:#0F5C5A; --pilloff:#F3E3BF; --pillofffg:#6B4A08; --danger:#B3402E;
         --heroopen:#F3E3BF; --onheroopen:#3D2B05; }
.dark  { --bg:#0F1515; --fg:#E2EAE8; --muted:#8FA09D; --card:#182221; --line:#26332F; --accent:#4DB3A8; --onaccent:#08201E;
         --pillon:#1D3432; --pillonfg:#7FD0C6; --pilloff:#3A2E14; --pillofffg:#EDC473; --danger:#E77C68;
         --heroopen:#3A2E14; --onheroopen:#F6E2B4; }
.sbar { display: flex; justify-content: space-between; align-items: center; padding: 16px 30px 6px; font-size: 14px;
        font-weight: 500; color: var(--fg); }
.cam { width: 12px; height: 12px; border-radius: 50%; background: #151B1A; }
.page { flex: 1; display: flex; flex-direction: column; padding: 14px 20px 22px; }
.title { display: flex; align-items: center; gap: 10px; font-size: 20px; font-weight: 500; padding: 4px 0 18px; }
.hero { border-radius: 22px; padding: 18px; margin-bottom: 18px; }
.hero.locked { background: #0F5C5A; color: #F2FBFA; }
.hero.open { background: var(--heroopen); color: var(--onheroopen); }
.hero .big { font-size: 28px; font-weight: 500; }
.hero .sub { font-size: 14px; opacity: .85; padding: 4px 0 8px; }
.chip { display: inline-block; font-family: 'Roboto Mono', monospace; font-size: 13px; padding: 4px 8px; border-radius: 8px; }
.locked .chip { background: rgba(255,255,255,.14); }
.open .chip { background: rgba(0,0,0,.08); }
.dark .open .chip { background: rgba(255,255,255,.08); }
.label { display: flex; justify-content: space-between; font-size: 12px; color: var(--muted); padding: 0 4px 8px; }
.label b { font-weight: 500; letter-spacing: .08em; }
.list { background: var(--card); border: 1px solid var(--line); border-radius: 16px; margin-bottom: 16px; }
.row { display: flex; justify-content: space-between; align-items: center; padding: 12px 14px; font-size: 15px; color: var(--muted); }
.row + .row { border-top: 1px solid var(--line); }
.pill { font-size: 12px; font-weight: 500; padding: 3px 10px; border-radius: 99px; }
.on { background: var(--pillon); color: var(--pillonfg); }
.off { background: var(--pilloff); color: var(--pillofffg); }
.grow { flex: 1; }
.btn { border-radius: 99px; text-align: center; padding: 14px; font-size: 15px; font-weight: 500; }
.primary { background: var(--accent); color: var(--onaccent); }
.text { color: var(--danger); }
.note { font-size: 13px; color: var(--muted); padding: 0 4px 12px; line-height: 1.4; }
.msg { font-size: 14px; padding: 0 4px 16px; }
.back { font-size: 18px; font-weight: 500; padding: 4px 0 26px; }
.h { text-align: center; font-size: 18px; font-weight: 500; margin-bottom: 4px; }
.s { text-align: center; font-size: 13px; color: var(--muted); margin-bottom: 16px; }
.groups { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; margin-bottom: 8px; }
.cell { height: 44px; border-radius: 10px; border: 1px solid var(--line); background: var(--card); display: flex;
        align-items: center; justify-content: center; font-family: 'Roboto Mono', monospace; font-size: 18px; letter-spacing: .05em; }
.cell.cur { border: 2px solid var(--accent); }
.cell i { font-style: normal; color: var(--muted); }
.meta { display: flex; justify-content: space-between; font-size: 13px; color: var(--muted); padding: 4px 0 8px; }
.meta b { color: var(--accent); font-weight: 500; }
.hint { text-align: center; font-size: 12px; color: var(--muted); }
.pad { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
.key { height: 58px; display: flex; align-items: center; justify-content: center; font-size: 24px; border-radius: 14px;
       background: var(--card); border: 1px solid var(--line); }
.key.fn { background: none; border: none; font-size: 18px; color: var(--muted); }
.key.ok { color: var(--accent); font-weight: 500; }
"""


def sbar():
    return '<div class="sbar"><span>9:41</span><span class="cam"></span><span>5G ▮▮▮</span></div>'


def title():
    return f'<div class="title">{ICON.format(s=30)}fckdigression</div>'


def rows(items, blocked):
    cls, txt = ("on", "Blocked") if blocked else ("off", "Allowed")
    return '<div class="list">' + "".join(
        f'<div class="row"><span>{i}</span><span class="pill {cls}">{txt}</span></div>' for i in items) + "</div>"


ROWS = ["Private DNS changes", "VPN apps", "Factory reset", "USB debugging", "Extra users"]

SCREEN_LOCKED = f"""{sbar()}<div class="page">{title()}
<div class="hero locked"><div class="big">Protected</div><div class="sub">Private DNS is locked to</div>
<span class="chip">dns.example.com</span></div>
<div class="label"><b>DNS GUARD</b><span>on</span></div>
{rows(ROWS, True)}
<div class="grow"></div><div class="btn primary">Enter parent code</div></div>"""

cells = '<div class="cell">•••••</div>' * 3 + '<div class="cell cur">•••<i>_</i></div>' + '<div class="cell"></div>' * 2
keys = "".join(f'<div class="key">{k}</div>' for k in "123456789")
SCREEN_PIN = f"""{sbar()}<div class="page"><div class="back">←&nbsp; Parent code</div>
<div class="h">Enter the parent code</div><div class="s">Have it read out to you in groups of 5</div>
<div class="groups">{cells}</div>
<div class="meta"><span>18 / 30 digits</span><b>Show digits</b></div>
<div class="hint">5 attempts left · a wrong length doesn't count</div>
<div class="grow"></div>
<div class="pad">{keys}<div class="key fn">⌫</div><div class="key">0</div><div class="key fn ok">OK</div></div></div>"""

SCREEN_UNLOCKED = f"""{sbar()}<div class="page">{title()}
<div class="hero open"><div class="big">Unlocked</div><div class="sub">Settings can be changed. Private DNS is still set to</div>
<span class="chip">dns.example.com</span></div>
<div class="label"><b>DNS GUARD</b><span>paused</span></div>
{rows(ROWS[:4], False)}
<div class="msg">Unlocked. Settings can be changed until you lock again.</div>
<div class="grow"></div>
<div class="btn primary" style="margin-bottom:4px">Lock again</div>
<div class="btn text">Remove fckdigression from this phone…</div></div>"""


def phones_html():
    def phone(theme, body, x, y, scale):
        return (f'<div class="phone" style="left:{x}px;top:{y}px;transform:scale({scale})">'
                f'<div class="screen {theme}">{body}</div></div>')
    s = 0.74
    return f"""<!doctype html><meta charset="utf-8">{FONTS}<style>{BASE_CSS}{PHONE_CSS}</style>
{phone("light", SCREEN_PIN, 130, 70, s)}
{phone("dark", SCREEN_UNLOCKED, 790, 70, s)}
{phone("light", SCREEN_LOCKED, 455, 40, s * 1.05)}"""

# --------------------------------------------------------------------------- terminal

SGR = re.compile(r"\x1b\[([0-9;]*)m")


def ansi_to_html(text):
    out, fg, bg, bold, pos = [], None, None, False, 0

    def emit(s):
        if not s:
            return
        style = []
        if fg:
            style.append(f"color:rgb({fg})")
        if bg:
            style.append(f"background:rgb({bg})")
        if bold:
            style.append("font-weight:700")
        s = html.escape(s)
        out.append(f'<span style="{";".join(style)}">{s}</span>' if style else s)

    for m in SGR.finditer(text):
        emit(text[pos:m.start()])
        pos = m.end()
        codes = [c for c in m.group(1).split(";") if c != ""] or ["0"]
        i = 0
        while i < len(codes):
            c = codes[i]
            if c == "0":
                fg = bg = None
                bold = False
            elif c == "1":
                bold = True
            elif c == "22":
                bold = False
            elif c == "39":
                fg = None
            elif c == "49":
                bg = None
            elif c in ("38", "48") and i + 4 < len(codes) and codes[i + 1] == "2":
                rgb = ",".join(codes[i + 2:i + 5])
                if c == "38":
                    fg = rgb
                else:
                    bg = rgb
                i += 4
            i += 1
    emit(text[pos:])
    return "".join(out)


TERM_CSS = """
.win { position: absolute; background: #101615; border-radius: 12px; overflow: hidden;
       box-shadow: 0 30px 60px rgba(15,60,55,.28), 0 6px 14px rgba(15,60,55,.18); border: 1px solid #1f2927; }
.bar { height: 34px; display: flex; align-items: center; gap: 8px; padding: 0 14px; background: #161E1D;
       font: 12px 'JetBrains Mono', monospace; color: #6E7C7A; }
.bar i { width: 11px; height: 11px; border-radius: 50%; background: #2C3836; }
.bar span { margin-left: 10px; }
pre { margin: 0; padding: 4px 18px 16px; font: 12px/1.42 'JetBrains Mono', monospace; color: #D5DEDC; white-space: pre; }
"""


def terminal_html():
    def win(name, x, y, title):
        with open(os.path.join(SRC, name), encoding="utf-8") as f:
            body = ansi_to_html(f.read().strip("\n"))
        return (f'<div class="win" style="left:{x}px;top:{y}px"><div class="bar"><i></i><i></i><i></i>'
                f'<span>{title}</span></div><pre>{body}</pre></div>')
    return f"""<!doctype html><meta charset="utf-8"><style>{BASE_CSS}{TERM_CSS}</style>
{win("tui-dashboard.ansi", 36, 34, "fckd")}
{win("tui-locked.ansi", 584, 236, "fckd — lock")}"""

# --------------------------------------------------------------------------- render


def render(page, name):
    """Firefox headless: the page is laid out at 1200x675 and zoomed 2x into a 2400x1350 window."""
    if not shutil.which("firefox"):
        sys.exit("firefox not found")
    page = page.replace("<style>", "<style>html { zoom: 2; }", 1)
    with tempfile.TemporaryDirectory() as tmp:
        src = os.path.join(tmp, "page.html")
        with open(src, "w", encoding="utf-8") as f:
            f.write(page)
        out = os.path.join(OUT, name)
        os.makedirs(os.path.join(tmp, "profile"))
        subprocess.run(["firefox", "--headless", "--no-remote", "--profile", os.path.join(tmp, "profile"),
                        "--window-size=2400,1350", "--screenshot", out, "file://" + src],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=120)
        print("wrote", out)


if __name__ == "__main__":
    render(phones_html(), "phones.png")
    render(terminal_html(), "terminal.png")
