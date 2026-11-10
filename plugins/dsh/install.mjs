import { createRequire } from "node:module";
import { readFile, mkdir, copyFile, chmod } from "node:fs/promises";
import { createHash } from "node:crypto";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));
const os = process.platform === "win32" ? "windows" : process.platform;
const arch = process.arch === "x64" ? "amd64" : process.arch;
const require = createRequire(import.meta.url);
const native = dirname(require.resolve(`dsh-reed-${os}-${arch}/package.json`));
const binary = process.platform === "win32" ? "swe.exe" : "swe";
const runtime = JSON.parse(await readFile(join(native, "runtime.json"), "utf8"));
const digest = createHash("sha256").update(await readFile(join(native, binary))).digest("hex");
if (digest !== runtime.assets[`${os}/${arch}`]?.sha256) throw new Error("ReedHarness native binary checksum mismatch");
await mkdir(join(root, "tools/swe/bin"), { recursive: true });
await copyFile(join(native, binary), join(root, "tools/swe/bin", binary));
await chmod(join(root, "tools/swe/bin", binary), 0o755);
await copyFile(join(native, "runtime.json"), join(root, "tools/swe/runtime.json"));
