"""Render the share card with resvg and a Korean font.

nix shell --inputs-from . nixpkgs#python3 nixpkgs#resvg --command python3 scripts/generate-og-image.py /path/to/NotoSansCJK-VF.otf.ttc
"""

from pathlib import Path
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET


client = Path(__file__).resolve().parent.parent / "client"
namespace = "http://www.w3.org/2000/svg"
ET.register_namespace("", namespace)
svg = ET.fromstring('''
<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  <rect width="1200" height="630" fill="#000"/>
  <defs>
    <filter id="soft-title" x="-10%" y="-20%" width="120%" height="140%">
      <feGaussianBlur stdDeviation="2.5"/>
    </filter>
  </defs>
  <g font-family="Noto Sans CJK KR" font-size="152" font-weight="900">
    <text x="604" y="371" fill="#f4b8c8" filter="url(#soft-title)">부끄</text>
    <text x="882" y="371" fill="#fff">판</text>
  </g>
</svg>
''')
icon = ET.parse(client / "icon.svg").getroot()
icon.set("x", "140")
icon.set("y", "107")
icon.set("width", "416")
icon.set("height", "416")
svg.insert(1, icon)
with tempfile.TemporaryDirectory(prefix="bukupan-og-") as directory:
    source = Path(directory) / "card.svg"
    ET.ElementTree(svg).write(source, encoding="unicode")
    subprocess.run([
        "resvg", "--skip-system-fonts", "--use-font-file", sys.argv[1],
        str(source), str(client / "og-image.png"),
    ], check=True)
