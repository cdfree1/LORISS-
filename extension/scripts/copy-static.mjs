import { cp, mkdir } from "node:fs/promises";

const staticFiles = ["manifest.json", "popup.html", "popup.css"];

await mkdir("dist", { recursive: true });
await Promise.all(staticFiles.map((file) => cp(file, `dist/${file}`)));
