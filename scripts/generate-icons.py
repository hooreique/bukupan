"""Generate install icons: nix shell --inputs-from . nixpkgs#python3 nixpkgs#resvg --command python3 scripts/generate-icons.py"""

from pathlib import Path
import subprocess
import tempfile
import xml.etree.ElementTree as ET


client = Path(__file__).resolve().parent.parent / "client"
namespace = "http://www.w3.org/2000/svg"
ET.register_namespace("", namespace)
original = ET.parse(client / "icon.svg").getroot()
background = original.find(f"{{{namespace}}}ellipse").attrib["fill"]
icon = ET.Element(f"{{{namespace}}}svg", {"viewBox": "0 0 512 512"})
ET.SubElement(icon, f"{{{namespace}}}rect", {
    "width": "512", "height": "512", "fill": background,
})
icon.append(original.find(f"{{{namespace}}}path"))

with tempfile.TemporaryDirectory(prefix="bukupan-icons-") as directory:
    source = Path(directory) / "install.svg"
    ET.ElementTree(icon).write(source, encoding="unicode")
    for size, filename in (
        (180, "apple-touch-icon.png"),
        (192, "pwa-192.png"),
        (512, "pwa-512.png"),
    ):
        subprocess.run([
            "resvg", "--width", str(size), "--height", str(size),
            str(source), str(client / filename),
        ], check=True)
