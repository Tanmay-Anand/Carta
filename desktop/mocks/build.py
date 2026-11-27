"""Builds the layout mocks into self-contained pages.

The sources in src/ share data.js and load JetBrains Mono from the repo. A
page opened on its own (attached, previewed, moved) cannot reach either, so
each built page carries the data and the font inline.
"""
import base64, pathlib, re

here = pathlib.Path(__file__).parent
fonts = here.parent.parent / "internal/ui/assets/fonts"
data = (here / "src/data.js").read_text(encoding="utf-8")

def font_uri(m):
    raw = (fonts / pathlib.Path(m.group(1)).name).read_bytes()
    return 'url("data:font/woff2;base64,' + base64.b64encode(raw).decode() + '")'

for src in sorted((here / "src").glob("*.html")):
    page = src.read_text(encoding="utf-8")
    page = page.replace('<script src="data.js"></script>', "<script>\n" + data + "</script>")
    page = re.sub(r'url\("(?:\.\./)+internal/ui/assets/fonts/([^"]+)"\)', font_uri, page)
    (here / src.name).write_text(page, encoding="utf-8", newline="\n")
    print("built", src.name, len(page) // 1024, "KB")
