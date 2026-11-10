import { mkdir, cp, writeFile, readFile, mkdtemp, rm, rename, readdir, unlink } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const version = process.argv[2];
if (!/^\d+\.\d+\.\d+(?:-[\w.-]+)?$/.test(version)) throw new Error("plugin version must be SemVer without v prefix");
const dist = join(root, "apps/swe-cli/dist");
const staging = await mkdtemp(join(dist, ".plugin-build-"));
const packages = join(staging, "packages");
const platforms = ["darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64", "windows_amd64", "windows_arm64"];
const optionalDependencies = {};
// Bundled Skills follow the repository directory layout; no registry is parsed.
const skillNames = (await readdir(join(root, "skills"), { withFileTypes: true }))
  .filter(entry => entry.isDirectory())
  .map(entry => entry.name)
  .sort();
async function addCLIInstructions(target) {
  for (const name of skillNames) {
    const entry = join(target, "skills", name, "SKILL.md");
    const content = await readFile(entry, "utf8");
    await writeFile(entry, content + "\n## 插件宿主 CLI 定位\n\n本插件附带独立 CLI。将当前 Skill 目录下 `../../tools/swe-cli/bin` 的绝对路径加入命令环境 PATH 后执行 `swe-cli`；也可直接执行该目录中的 `swe-cli`（Windows 为 `swe-cli.exe`）。保留工具目录相邻的 `runtime.json`。\n");
  }
}
try {
for (const platform of platforms) {
  const target = join(packages, platform);
  await mkdir(target, { recursive: true });
  await cp(join(root, "plugins/dsh"), target, { recursive: true });
  await mkdir(join(target, "tools"), { recursive: true });
  execFileSync("tar", ["-xzf", join(dist, `swe-cli_${platform}.tar.gz`), "-C", join(target, "tools")]);
  for (const name of skillNames) execFileSync("tar", ["-xzf", join(dist, `${name}.tar.gz`), "-C", target]);
  await addCLIInstructions(target);

  const manifest = { name: "reed", version, description: "SWE engineering skills and architecture tools", skills: "./skills/" };
  for (const host of ["codex", "claude"]) {
    await mkdir(join(target, `.${host}-plugin`), { recursive: true });
    await writeFile(join(target, `.${host}-plugin/plugin.json`), JSON.stringify(manifest, null, 2) + "\n");
  }
  const pkg = JSON.parse(await readFile(join(target, "package.json"), "utf8"));
  pkg.version = version;
  pkg.os = [platform.split("_")[0] === "windows" ? "win32" : platform.split("_")[0]];
  pkg.cpu = [platform.endsWith("amd64") ? "x64" : "arm64"];
  await writeFile(join(target, "package.json"), JSON.stringify(pkg, null, 2) + "\n");
  execFileSync("tar", ["-czf", join(staging, `reed-plugin_${platform}.tar.gz`), "-C", target, "."]);
  const name = "dsh-reed-" + platform.replace("_", "-");
  optionalDependencies[name] = version;
  const native = join(packages, name);
  await mkdir(native, { recursive: true });
  const binary = platform.startsWith("windows") ? "swe-cli.exe" : "swe-cli";
  await cp(join(target, "tools/swe-cli/bin", binary), join(native, binary));
  await cp(join(target, "tools/swe-cli/runtime.json"), join(native, "runtime.json"));
  await writeFile(join(native, "package.json"), JSON.stringify({ name, version, os: pkg.os, cpu: pkg.cpu, files: [binary, "runtime.json"] }, null, 2) + "\n");
}
const npmRoot = join(packages, "dsh-reed");
await mkdir(npmRoot, { recursive: true });
await cp(join(root, "plugins/dsh"), npmRoot, { recursive: true });
for (const name of skillNames) {
  const sourceRoot = join(root, "skills", name);
  await cp(sourceRoot, join(npmRoot, "skills", name), { recursive: true, filter: source => source !== join(sourceRoot, "bin") && source !== join(sourceRoot, "runtime.json") });
}
await addCLIInstructions(npmRoot);
const npmPackage = JSON.parse(await readFile(join(npmRoot, "package.json"), "utf8"));
npmPackage.version = version;
npmPackage.optionalDependencies = optionalDependencies;
npmPackage.scripts = { postinstall: "node install.mjs" };
await writeFile(join(npmRoot, "package.json"), JSON.stringify(npmPackage, null, 2) + "\n");
await mkdir(join(dist, "plugins"), { recursive: true });
// Renamed or stale plugin archives never ship next to this build.
for (const file of await readdir(dist)) {
  if (/-plugin_.*\.tar\.gz$/.test(file)) await unlink(join(dist, file));
}
const destination = join(dist, "plugins", version);
const previous = join(staging, "previous");
let hadPrevious = false;
try { await rename(destination, previous); hadPrevious = true; } catch (error) { if (error.code !== "ENOENT") throw error; }
try { await rename(packages, destination); } catch (error) {
  if (hadPrevious) await rename(previous, destination);
  throw error;
}
for (const platform of platforms) {
  const archive = `reed-plugin_${platform}.tar.gz`;
  await rename(join(staging, archive), join(dist, archive));
}
} finally { await rm(staging, { recursive: true, force: true }); }
